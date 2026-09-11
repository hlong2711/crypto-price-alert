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

// SlackBotNotifier posts target-aware messages through Slack's Web API.
type SlackBotNotifier struct {
	botToken    string
	baseURL     string
	client      *http.Client
	maxAttempts int
	backoff     time.Duration
}

// TargetProvider identifies Slack destinations handled by this notifier.
func (*SlackBotNotifier) TargetProvider() domain.ChatProvider { return domain.ChatProviderSlack }

// NewSlackBotNotifier creates a Slack notifier that can address individual channels.
func NewSlackBotNotifier(botToken, baseURL string, client *http.Client, maxAttempts int, backoff time.Duration) (*SlackBotNotifier, error) {
	if botToken == "" || maxAttempts < 1 || backoff <= 0 {
		return nil, fmt.Errorf("invalid Slack bot notifier settings")
	}
	if baseURL == "" {
		baseURL = "https://slack.com/api"
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &SlackBotNotifier{
		botToken:    botToken,
		baseURL:     baseURL,
		client:      client,
		maxAttempts: maxAttempts,
		backoff:     backoff,
	}, nil
}

// SendToTarget posts a message to the target Slack channel.
func (n *SlackBotNotifier) SendToTarget(ctx context.Context, target domain.AlertTarget, message domain.Message) error {
	if target.Provider != domain.ChatProviderSlack || target.ExternalChatID == "" {
		return fmt.Errorf("invalid Slack alert target")
	}
	return n.SendMessageToChannel(ctx, target.ExternalChatID, RenderMessage(message))
}

// Send implements the legacy notifier contract but requires an explicit target for Slack Bot delivery.
func (n *SlackBotNotifier) Send(context.Context, domain.Message) error {
	return fmt.Errorf("Slack bot notifier requires an alert target")
}

// SendMessageToChannel posts plain text to a specific Slack channel.
func (n *SlackBotNotifier) SendMessageToChannel(ctx context.Context, channelID, messageText string) error {
	payload, err := json.Marshal(struct {
		Channel string `json:"channel"`
		Text    string `json:"text"`
	}{Channel: channelID, Text: messageText})
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := range n.maxAttempts {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/chat.postMessage", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+n.botToken)
		request.Header.Set("Content-Type", "application/json")
		response, err := n.client.Do(request)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			if readErr == nil && response.StatusCode >= 200 && response.StatusCode < 300 {
				var result struct {
					OK    bool   `json:"ok"`
					Error string `json:"error"`
				}
				if json.Unmarshal(body, &result) == nil && result.OK {
					return nil
				}
				lastErr = fmt.Errorf("Slack API: %s", result.Error)
			} else if readErr != nil {
				lastErr = readErr
			} else {
				lastErr = fmt.Errorf("Slack API HTTP status %d", response.StatusCode)
			}
		} else {
			lastErr = err
		}
		if attempt < n.maxAttempts-1 {
			time.Sleep(n.backoff * time.Duration(1<<attempt))
		}
	}
	return lastErr
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
