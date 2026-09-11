package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/domain"

	"github.com/labstack/echo/v4"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fakeEvents struct {
	claimed   bool
	processed bool
	failed    bool
}

func (f *fakeEvents) ClaimInboundEvent(context.Context, domain.InboundEvent) (bool, error) {
	if f.claimed {
		return false, nil
	}
	f.claimed = true
	return true, nil
}
func (f *fakeEvents) MarkInboundEventProcessed(context.Context, domain.ChatProvider, string, time.Time) error {
	f.processed = true
	return nil
}
func (f *fakeEvents) MarkInboundEventFailed(context.Context, domain.ChatProvider, string, string) error {
	f.failed = true
	return nil
}

type fakeTargets struct{ target domain.AlertTarget }

func (f *fakeTargets) FindOrCreateTarget(_ context.Context, target domain.AlertTarget) (domain.AlertTarget, error) {
	f.target = target
	return target, nil
}
func (f *fakeTargets) GetTarget(context.Context, domain.ChatProvider, string, string) (domain.AlertTarget, error) {
	return f.target, nil
}

type fakeCommands struct {
	action  chat.CommandAction
	updates int
}

func (f *fakeCommands) Handle(_ context.Context, command chat.Command) (string, error) {
	f.action = command.Action
	if command.Action == chat.ActionConfigure {
		return "session-1", nil
	}
	return "ok", nil
}
func (f *fakeCommands) UpdateSession(context.Context, chat.Command, []string, []domain.Interval) error {
	f.updates++
	return nil
}

func newAdapter(t *testing.T, httpClient *http.Client, events *fakeEvents, commands *fakeCommands) *Adapter {
	t.Helper()
	client, err := NewAPIClient("token", "http://telegram.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, &fakeTargets{}, events, commands, chat.NewMemorySessionStore(), "secret", "bot", []string{"BTCUSDT"}, []domain.Interval{domain.Interval1H})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestAPIClientAndCommandRegistration(t *testing.T) {
	var methods []string
	var commandPayload []byte
	client, err := NewAPIClient("token", "http://telegram.test", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		methods = append(methods, r.URL.Path)
		commandPayload, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":[]}`)),
		}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RegisterDefaultCommands(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 || methods[0] != "/bottoken/setMyCommands" {
		t.Fatalf("unexpected API call: %v", methods)
	}
	var payload struct {
		Commands []BotCommand `json:"commands"`
	}
	if err := json.Unmarshal(commandPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Commands) != 6 || payload.Commands[1].Command != "configure" {
		t.Fatalf("unexpected command catalog: %+v", payload.Commands)
	}
}

func TestSetWebhook(t *testing.T) {
	var method string
	var payload struct {
		URL         string `json:"url"`
		SecretToken string `json:"secret_token"`
	}
	client, err := NewAPIClient("token", "http://telegram.test", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetWebhook(context.Background(), "https://alerts.example.com/api/v1/chat/telegram/webhook", "secret"); err != nil {
		t.Fatal(err)
	}
	if method != "/bottoken/setWebhook" || payload.URL != "https://alerts.example.com/api/v1/chat/telegram/webhook" || payload.SecretToken != "secret" {
		t.Fatalf("unexpected setWebhook request: method=%s payload=%+v", method, payload)
	}
}

func TestWebhookRejectsSecretAndDuplicate(t *testing.T) {
	apiClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"ok":true,"result":[]}`
		if strings.HasSuffix(r.URL.Path, "/getChatAdministrators") {
			body = `{"ok":true,"result":[{"user":{"id":7},"status":"creator"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	events := &fakeEvents{}
	commands := &fakeCommands{}
	adapter := newAdapter(t, apiClient, events, commands)
	e := echo.New()
	payload, _ := json.Marshal(Update{
		UpdateID: 42,
		Message: &Message{
			From: &User{ID: 7},
			Chat: Chat{ID: 9, Type: "group"},
			Text: "/crypto-alert help",
		},
	})
	request := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(payload)))
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(request, recorder)
	if err := adapter.Webhook(ctx); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized, got %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(payload)))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	recorder = httptest.NewRecorder()
	ctx = e.NewContext(request, recorder)
	if err := adapter.Webhook(ctx); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !events.processed || events.failed {
		t.Fatalf("unexpected first webhook result: code=%d processed=%t failed=%t", recorder.Code, events.processed, events.failed)
	}

	request = httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(payload)))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	recorder = httptest.NewRecorder()
	ctx = e.NewContext(request, recorder)
	if err := adapter.Webhook(ctx); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected duplicate acknowledgment, got %d", recorder.Code)
	}
}
