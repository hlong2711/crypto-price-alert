package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// VerifySignature authenticates a raw Slack request and rejects replayed timestamps.
func VerifySignature(signingSecret string, timestamp string, signature string, body []byte, now time.Time, replayWindow time.Duration) error {
	if strings.TrimSpace(signingSecret) == "" || strings.TrimSpace(timestamp) == "" || strings.TrimSpace(signature) == "" || replayWindow <= 0 {
		return fmt.Errorf("invalid Slack signature settings")
	}

	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || now.Sub(time.Unix(seconds, 0)) > replayWindow || time.Unix(seconds, 0).Sub(now) > replayWindow {
		return fmt.Errorf("stale Slack request")
	}

	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write([]byte("v0:" + timestamp + ":" + string(body)))
	expected := "v0=" + fmt.Sprintf("%x", mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return fmt.Errorf("invalid Slack signature")
	}
	return nil
}
