package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"crypto-price-alert/internal/domain"
)

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
	return &SlackNotifier{
		webhookURL:  webhookURL,
		client:      client,
		maxAttempts: maxAttempts,
		backoff:     backoff}, nil
}

func (n *SlackNotifier) Send(ctx context.Context, message domain.Message) error {
	return n.SendMessage(ctx, RenderMessage(message))
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
