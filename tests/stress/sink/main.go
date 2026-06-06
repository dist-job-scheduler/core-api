// Command sink is a controllable HTTP target + recorder for buffer stress
// tests. The buffers under test point their URL at /ingest; the scheduler's
// executor calls it for every (re)delivery. The sink records each delivery
// keyed by the X-Stress-Token header and answers per a per-token policy that
// k6 configures over /control/*.
//
// It is intentionally dependency-free (stdlib only) and knows nothing about
// core-api internals — it is part of the harness, not the system under test.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const tokenHeader = "X-Stress-Token"

type delivery struct {
	Token  string `json:"token"`
	TsMs   int64  `json:"ts_ms"`
	Status int    `json:"status"`
	Method string `json:"method"`
}

// policy controls how the sink responds to a given token.
//
//	mode "ok"          -> always 200
//	mode "always_fail" -> always 500
//	mode "fail_n"      -> first N deliveries 500, then 200 (exercises retry->success)
//	mode "sleep"       -> sleep SleepMs then return Status (default 200); exercises timeouts
//	mode "status"      -> return Status verbatim (e.g. 429 with Retry-After)
type policy struct {
	Mode       string `json:"mode"`
	N          int    `json:"n"`
	SleepMs    int    `json:"sleep_ms"`
	Status     int    `json:"status"`
	RetryAfter int    `json:"retry_after"`
}

type store struct {
	mu          sync.Mutex
	deliveries  []delivery
	policies    map[string]policy
	defaultPol  policy // applied when a token has no explicit policy
	seen        map[string]int // token -> times delivered (drives fail_n)
}

func newStore() *store {
	return &store{
		policies:   map[string]policy{},
		defaultPol: policy{Mode: "ok"},
		seen:       map[string]int{},
	}
}

func (s *store) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deliveries = nil
	s.policies = map[string]policy{}
	s.defaultPol = policy{Mode: "ok"}
	s.seen = map[string]int{}
}

func (s *store) setDefault(p policy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultPol = p
}

// decide records the delivery and returns the status to respond with, plus any
// Retry-After seconds (0 = none).
func (s *store) decide(token, method string) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seen[token]++
	n := s.seen[token]
	p, ok := s.policies[token]
	if !ok {
		p = s.defaultPol
	}

	status := http.StatusOK
	retryAfter := 0
	sleepMs := 0

	switch p.Mode {
	case "", "ok":
		status = http.StatusOK
	case "always_fail":
		status = http.StatusInternalServerError
	case "fail_n":
		if n <= p.N {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusOK
		}
	case "sleep":
		sleepMs = p.SleepMs
		status = p.Status
		if status == 0 {
			status = http.StatusOK
		}
	case "status":
		status = p.Status
		if status == 0 {
			status = http.StatusOK
		}
		retryAfter = p.RetryAfter
	default:
		status = http.StatusOK
	}

	s.deliveries = append(s.deliveries, delivery{
		Token:  token,
		TsMs:   time.Now().UnixMilli(),
		Status: status,
		Method: method,
	})

	// Sleep outside the lock would be better, but keeping the critical section
	// simple is fine for light loads; release then sleep.
	if sleepMs > 0 {
		s.mu.Unlock()
		time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		s.mu.Lock()
	}

	return status, retryAfter
}

func (s *store) deliveriesFor(token string) []delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []delivery{}
	for _, d := range s.deliveries {
		if token == "" || d.Token == token {
			out = append(out, d)
		}
	}
	return out
}

func (s *store) setPolicy(token string, p policy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[token] = p
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9000"
	}
	s := newStore()
	mux := http.NewServeMux()

	mux.HandleFunc("/ingest", func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(tokenHeader)
		if token == "" {
			// Fall back to the body so callers that can't set headers still work.
			b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
			token = string(b)
		}
		status, retryAfter := s.decide(token, r.Method)
		if retryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		}
		w.WriteHeader(status)
	})

	mux.HandleFunc("/control/reset", func(w http.ResponseWriter, r *http.Request) {
		s.reset()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/control/policy", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token string `json:"token"`
			policy
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Token == "" {
			http.Error(w, "token required", http.StatusBadRequest)
			return
		}
		s.setPolicy(body.Token, body.policy)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/control/default", func(w http.ResponseWriter, r *http.Request) {
		var p policy
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.setDefault(p)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/deliveries", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		ds := s.deliveriesFor(token)
		writeJSON(w, map[string]any{"token": token, "count": len(ds), "deliveries": ds})
	})

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		all := s.deliveriesFor("")
		perToken := map[string]int{}
		ack := map[string]int{} // 2xx acks per token
		for _, d := range all {
			perToken[d.Token]++
			if d.Status >= 200 && d.Status < 300 {
				ack[d.Token]++
			}
		}
		writeJSON(w, map[string]any{
			"total_deliveries": len(all),
			"distinct_tokens":  len(perToken),
			"per_token":        perToken,
			"acks_per_token":   ack,
		})
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("sink listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
