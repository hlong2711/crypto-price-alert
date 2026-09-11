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

// TargetProvider identifies Telegram destinations handled by this notifier.
func (*TelegramNotifier) TargetProvider() domain.ChatProvider { return domain.ChatProviderTelegram }

// SendToTarget delivers through Telegram using the target's external chat ID.
func (n *TelegramNotifier) SendToTarget(ctx context.Context, target domain.AlertTarget, message domain.Message) error {
	if target.Provider != domain.ChatProviderTelegram || target.ExternalChatID == "" {
		return fmt.Errorf("invalid Telegram alert target")
	}
	return n.sendMessageToChat(ctx, target.ExternalChatID, RenderMessage(message))
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
	return n.sendMessageToChat(ctx, n.chatID, messageText)
}

func (n *TelegramNotifier) sendMessageToChat(ctx context.Context, chatID, messageText string) error {
	payload, err := json.Marshal(telegramPayload{ChatID: chatID, Text: messageText})
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
