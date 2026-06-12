package metrics

import (
	"encoding/json"
	"net/http"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/health"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Worker metrics

	JobPickupLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Name:      "job_pickup_latency_seconds",
		Help:      "Time from job creation to worker claiming it.",
		Buckets:   []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	})

	JobExecutionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Name:      "job_execution_duration_seconds",
		Help:      "Duration of job HTTP execution.",
		Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"status"})

	JobsInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "scheduler",
		Name:      "worker_jobs_in_flight",
		Help:      "Number of jobs currently being executed by the worker.",
	})

	JobsCompletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "jobs_completed_total",
		Help:      "Total jobs finished, by outcome.",
	}, []string{"outcome"})

	// Reaper metrics

	ReaperRescuedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "reaper_rescued_total",
		Help:      "Total stale jobs handled by the reaper.",
	}, []string{"action"})

	ReaperCycleDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Name:      "reaper_cycle_duration_seconds",
		Help:      "Time taken for one reaper cycle.",
		Buckets:   prometheus.DefBuckets,
	})

	// Worker lifecycle

	WorkerStartTime = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "scheduler",
		Name:      "worker_start_time_seconds",
		Help:      "Unix timestamp when the worker started.",
	})

	WorkerShutdownsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "worker_shutdowns_total",
		Help:      "Number of times the worker has shut down.",
	})

	// HTTP metrics

	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency.",
		Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"method", "path", "status"})

	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests.",
	}, []string{"method", "path", "status"})

	// Webhook metrics

	WebhookDeliveriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "webhook_deliveries_total",
		Help:      "Total webhook delivery attempts, by outcome.",
	}, []string{"outcome"})

	WebhookDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Name:      "webhook_duration_seconds",
		Help:      "Duration of webhook HTTP calls.",
		Buckets:   []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10},
	})

	// Alert metrics

	AlertDeliveriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "alert_deliveries_total",
		Help:      "Total failure-alert delivery attempts, by channel type and outcome.",
	}, []string{"type", "outcome"})

	// Buffer drainer metrics

	BufferItemsInFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "scheduler",
		Name:      "buffer_items_in_flight",
		Help:      "Number of buffer items currently being executed by the drainer.",
	})

	BufferItemsCompletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "buffer_items_completed_total",
		Help:      "Total buffer items finished, by outcome.",
	}, []string{"outcome"})

	BufferDrainerCycleDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Name:      "buffer_drainer_cycle_duration_seconds",
		Help:      "Time taken for one drainer cycle.",
		Buckets:   prometheus.DefBuckets,
	})

	BufferReaperRescuedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "buffer_reaper_rescued_total",
		Help:      "Total stale buffer items handled by the buffer reaper.",
	}, []string{"action"})

	BufferReaperCycleDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "scheduler",
		Name:      "buffer_reaper_cycle_duration_seconds",
		Help:      "Time taken for one buffer reaper cycle.",
		Buckets:   prometheus.DefBuckets,
	})

	// Rate limiting

	RateLimitExceededTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "scheduler",
		Name:      "rate_limit_exceeded_total",
		Help:      "Total requests rejected by rate limiting.",
	})
)

func Register() {
	prometheus.MustRegister(
		JobPickupLatency,
		JobExecutionDuration,
		JobsInFlight,
		JobsCompletedTotal,
		ReaperRescuedTotal,
		ReaperCycleDuration,
		WorkerStartTime,
		WorkerShutdownsTotal,
		HTTPRequestDuration,
		HTTPRequestsTotal,
		WebhookDeliveriesTotal,
		WebhookDuration,
		AlertDeliveriesTotal,
		BufferItemsInFlight,
		BufferItemsCompletedTotal,
		BufferDrainerCycleDuration,
		BufferReaperRescuedTotal,
		BufferReaperCycleDuration,
		RateLimitExceededTotal,
	)
}

func NewServer(addr string, checker *health.Checker) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeHealth(w, checker.Liveness(r.Context()))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeHealth(w, checker.Readiness(r.Context()))
	})

	return &http.Server{Addr: addr, Handler: mux}
}

func writeHealth(w http.ResponseWriter, result health.HealthResult) {
	w.Header().Set("Content-Type", "application/json")
	if result.Status != "up" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(result)
}
