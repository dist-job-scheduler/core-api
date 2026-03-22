package scheduler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// signRequest computes the Fliq webhook signature.
// Returns the timestamp string and the "v1=<hex>" signature.
func signRequest(secret, method, url string, body []byte, now time.Time) (timestamp, signature string) {
	ts := strconv.FormatInt(now.UTC().Unix(), 10)

	bodyStr := ""
	if len(body) > 0 {
		bodyStr = string(body)
	}

	payload := fmt.Sprintf("%s.%s.%s.%s", ts, method, url, bodyStr)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := "v1=" + hex.EncodeToString(mac.Sum(nil))

	return ts, sig
}
