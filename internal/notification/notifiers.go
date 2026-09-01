package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"crypto-price-alert/internal/domain"
)

type TelegramNotifier struct {
	botToken    string
	chatID      string
	baseURL     string
	client      *http.Client
	maxAttempts int
	backoff     time.Duration
}

func NewTelegramNotifier(botToken, chatID, baseURL string, client *http.Client, maxAttempts int, backoff time.Duration) (*TelegramNotifier, error) {
	if botToken == "" || chatID == "" || maxAttempts < 1 || backoff <= 0 {
		return nil, fmt.Errorf("invalid Telegram notifier settings")
	}
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &TelegramNotifier{botToken: botToken, chatID: chatID, baseURL: baseURL, client: client, maxAttempts: maxAttempts, backoff: backoff}, nil
}

func (n *TelegramNotifier) Send(ctx context.Context, message domain.Message) error {
	return n.SendMessage(ctx, RenderMessage(message))
}

type telegramPayload struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

func (n *TelegramNotifier) SendMessage(ctx context.Context, messageText string) error {
	payload, err := json.Marshal(telegramPayload{ChatID: n.chatID, Text: messageText})
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", n.baseURL, n.botToken)
	return sendWithRetry(ctx, n.client, http.MethodPost, url, payload, n.maxAttempts, n.backoff)
}

type SlackNotifier struct {
	webhookURL  string
	client      *http.Client
	maxAttempts int
	backoff     time.Duration
}

func NewSlackNotifier(webhookURL string, client *http.Client, maxAttempts int, backoff time.Duration) (*SlackNotifier, error) {
	if webhookURL == "" || maxAttempts < 1 || backoff <= 0 {
		return nil, fmt.Errorf("invalid Slack notifier settings")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &SlackNotifier{webhookURL: webhookURL, client: client, maxAttempts: maxAttempts, backoff: backoff}, nil
}

func (n *SlackNotifier) SendMessage(ctx context.Context, messageText string) error {
	payload, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: messageText})
	if err != nil {
		return err
	}
	return sendWithRetry(ctx, n.client, http.MethodPost, n.webhookURL, payload, n.maxAttempts, n.backoff)
}

func (n *SlackNotifier) Send(ctx context.Context, message domain.Message) error {
	return n.SendMessage(ctx, RenderMessage(message))
}

func sendWithRetry(ctx context.Context, client *http.Client, method, url string, payload []byte, maxAttempts int, backoff time.Duration) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
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
