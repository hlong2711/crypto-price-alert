package notification

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

func sendWithRetry(ctx context.Context, client *http.Client, method, url string, payload []byte, maxAttempts int, backoff time.Duration) error {
	var lastErr error
	for attempt := range maxAttempts {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			} else {
				lastErr = fmt.Errorf("notification HTTP status %d: %s", resp.StatusCode, string(body))
				if resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
					return lastErr
				}
			}
		}
		if attempt == maxAttempts-1 {
			break
		}
		timer := time.NewTimer(backoff * time.Duration(1<<attempt))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	return lastErr
}
