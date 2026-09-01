package notification

import (
	"context"
	"encoding/json"
	"fmt"
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
	return &TelegramNotifier{
		botToken:    botToken,
		chatID:      chatID,
		baseURL:     baseURL,
		client:      client,
		maxAttempts: maxAttempts,
		backoff:     backoff}, nil
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
	return sendWithRetry(ctx,
		n.client,
		http.MethodPost,
		fmt.Sprintf("%s/bot%s/sendMessage", n.baseURL, n.botToken),
		payload,
		n.maxAttempts,
		n.backoff,
	)
}
