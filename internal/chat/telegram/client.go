package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type APIClient struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

func NewAPIClient(token, baseURL string, httpClient *http.Client) (*APIClient, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("telegram bot token is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.telegram.org"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &APIClient{token: token, baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}, nil
}

func (c *APIClient) GetChatAdministrators(ctx context.Context, chatID int64) ([]ChatMember, error) {
	var result []ChatMember
	err := c.call(ctx, "getChatAdministrators", map[string]any{"chat_id": chatID}, &result)
	return result, err
}

func (c *APIClient) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	return c.call(ctx, "setMyCommands", map[string]any{"commands": commands}, nil)
}

// RegisterDefaultCommands publishes the Telegram command menu for discoverability.
func (c *APIClient) RegisterDefaultCommands(ctx context.Context) error {
	return c.SetMyCommands(ctx, commandList)
}

func (c *APIClient) SendMessage(ctx context.Context, chatID int64, text string, keyboard *InlineKeyboardMarkup) error {
	payload := map[string]any{"chat_id": chatID, "text": text}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}
	return c.call(ctx, "sendMessage", payload, nil)
}

func (c *APIClient) AnswerCallbackQuery(ctx context.Context, callbackID string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": callbackID}, nil)
}

func (c *APIClient) call(ctx context.Context, method string, payload any, result any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram API %s: status %d", method, resp.StatusCode)
	}
	var envelope apiResponse[json.RawMessage]
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return fmt.Errorf("decode telegram API %s: %w", method, err)
	}
	if !envelope.OK {
		return fmt.Errorf("telegram API %s: %s", method, envelope.Description)
	}
	if result != nil && len(envelope.Result) > 0 {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("decode telegram API result %s: %w", method, err)
		}
	}
	return nil
}
