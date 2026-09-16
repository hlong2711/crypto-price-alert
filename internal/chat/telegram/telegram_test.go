package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/domain"

	"github.com/labstack/echo/v4"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type telegramAPICall struct {
	Method  string
	Payload map[string]any
}

func newTelegramAPIRecorder(t *testing.T, calls *[]telegramAPICall, creatorID int64) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		method := strings.TrimPrefix(r.URL.Path, "/bottoken/")
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
		}
		*calls = append(*calls, telegramAPICall{Method: method, Payload: payload})

		response := `{"ok":true,"result":true}`
		if method == "getChatAdministrators" {
			response = `{"ok":true,"result":[{"user":{"id":` + chatIDString(creatorID) + `},"status":"creator"}]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})}
}

type fakeEvents struct {
	claimed   bool
	seen      map[string]bool
	message   string
	processed bool
	failed    bool
}

func (f *fakeEvents) ClaimInboundEvent(_ context.Context, event domain.InboundEvent) (bool, error) {
	if f.seen == nil {
		f.seen = make(map[string]bool)
	}
	if f.seen[event.ExternalEventID] {
		return false, nil
	}
	f.seen[event.ExternalEventID] = true
	f.claimed = true
	f.message = event.Message
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
	sessions *chat.MemorySessionStore
	action   chat.CommandAction
	updates  int
}

func (f *fakeCommands) Handle(_ context.Context, command chat.Command) (string, error) {
	f.action = command.Action
	if command.Action == chat.ActionConfigure {
		if f.sessions != nil {
			_ = f.sessions.Create(context.Background(), chat.ConfigSession{
				SessionID:         "session-1",
				TargetID:          command.Target.ID,
				ActorUserID:       command.ActorUserID,
				BaseConfigVersion: 1,
				ExpiresAt:         time.Now().Add(time.Hour),
			})
		}
		return "session-1", nil
	}
	if (command.Action == chat.ActionSave || command.Action == chat.ActionCancel) && f.sessions != nil && len(command.Arguments) == 1 {
		_ = f.sessions.Delete(context.Background(), command.Arguments[0])
	}
	return "ok", nil
}
func (f *fakeCommands) UpdateSession(ctx context.Context, command chat.Command, symbols []string, intervals []domain.Interval) error {
	f.updates++
	if f.sessions != nil && len(command.Arguments) == 1 {
		session, err := f.sessions.Get(ctx, command.Arguments[0])
		if err != nil {
			return err
		}
		session.SelectedSymbols = append([]string(nil), symbols...)
		session.SelectedIntervals = append([]domain.Interval(nil), intervals...)
		return f.sessions.Update(ctx, session)
	}
	return nil
}

func newAdapter(t *testing.T, httpClient *http.Client, events *fakeEvents, commands *fakeCommands) *Adapter {
	t.Helper()
	sessions := chat.NewMemorySessionStore()
	commands.sessions = sessions
	client, err := NewAPIClient("token", "http://telegram.test", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, &fakeTargets{}, events, commands, sessions, "secret", "bot", []string{"BTCUSDT"}, []domain.Interval{domain.Interval1H})
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func postTelegramUpdate(t *testing.T, adapter *Adapter, update Update) int {
	t.Helper()
	e := echo.New()
	payload, _ := json.Marshal(update)
	request := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(payload)))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(request, recorder)
	if err := adapter.Webhook(ctx); err != nil {
		t.Fatal(err)
	}
	return recorder.Code
}

func telegramMessage(updateID int64, text string) Update {
	return Update{
		UpdateID: updateID,
		Message: &Message{
			From: &User{ID: 7},
			Chat: Chat{ID: 9, Type: "group", Title: "alerts"},
			Text: text,
		},
	}
}

func telegramCallback(updateID int64, userID int64, action string) Update {
	return Update{
		UpdateID: updateID,
		CallbackQuery: &CallbackQuery{
			ID:   "callback-" + chatIDString(updateID),
			From: User{ID: userID},
			Message: &Message{
				MessageID: 99,
				Chat:      Chat{ID: 9, Type: "group", Title: "alerts"},
			},
			Data: callbackData("session-1", action),
		},
	}
}

func lastPayloadForMethod(t *testing.T, calls []telegramAPICall, method string) map[string]any {
	t.Helper()
	for _, call := range slices.Backward(calls) {
		if call.Method == method {
			return call.Payload
		}
	}
	t.Fatalf("missing Telegram API method %s in calls %+v", method, calls)
	return nil
}

func keyboardTexts(t *testing.T, payload map[string]any) []string {
	t.Helper()
	replyMarkup, ok := payload["reply_markup"].(map[string]any)
	if !ok {
		t.Fatalf("missing reply markup in payload %+v", payload)
	}
	rows, ok := replyMarkup["inline_keyboard"].([]any)
	if !ok {
		t.Fatalf("missing inline keyboard in payload %+v", payload)
	}
	var texts []string
	for _, row := range rows {
		buttons, ok := row.([]any)
		if !ok {
			t.Fatalf("invalid keyboard row %+v", row)
		}
		for _, button := range buttons {
			fields, ok := button.(map[string]any)
			if !ok {
				t.Fatalf("invalid keyboard button %+v", button)
			}
			text, _ := fields["text"].(string)
			texts = append(texts, text)
		}
	}
	return texts
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
	if events.message != "/crypto-alert help" {
		t.Fatalf("unexpected inbound message: %q", events.message)
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

func TestWebhookIgnoresMessagesWithoutCommandPrefix(t *testing.T) {
	events := &fakeEvents{}
	commands := &fakeCommands{}
	adapter := newAdapter(t, &http.Client{}, events, commands)
	e := echo.New()
	payload, _ := json.Marshal(Update{
		UpdateID: 43,
		Message: &Message{
			From: &User{ID: 7},
			Chat: Chat{ID: 9, Type: "group"},
			Text: "please configure alerts",
		},
	})
	request := httptest.NewRequest(http.MethodPost, "/telegram", strings.NewReader(string(payload)))
	request.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	recorder := httptest.NewRecorder()
	ctx := e.NewContext(request, recorder)

	if err := adapter.Webhook(ctx); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected ignored message acknowledgment, got %d", recorder.Code)
	}
	if events.claimed || events.processed || events.failed {
		t.Fatalf("non-command message should not enter inbound event processing: %+v", events)
	}
}

func TestConfigureSendsSymbolKeyboardFirst(t *testing.T) {
	var calls []telegramAPICall
	events := &fakeEvents{}
	commands := &fakeCommands{}
	adapter := newAdapter(t, newTelegramAPIRecorder(t, &calls, 7), events, commands)

	if code := postTelegramUpdate(t, adapter, telegramMessage(100, "/crypto-alert configure")); code != http.StatusOK {
		t.Fatalf("unexpected status %d", code)
	}

	payload := lastPayloadForMethod(t, calls, "sendMessage")
	if got, want := payload["text"], "Select symbols for alerts.\nSelected: none"; got != want {
		t.Fatalf("unexpected configure message text: got %q want %q", got, want)
	}
	texts := keyboardTexts(t, payload)
	if strings.Join(texts, ",") != "BTCUSDT,Next,Cancel" {
		t.Fatalf("unexpected symbol keyboard: %v", texts)
	}
}

func TestSymbolCallbackUpdatesSessionAndKeepsSymbolScreen(t *testing.T) {
	var calls []telegramAPICall
	events := &fakeEvents{}
	commands := &fakeCommands{}
	adapter := newAdapter(t, newTelegramAPIRecorder(t, &calls, 7), events, commands)
	postTelegramUpdate(t, adapter, telegramMessage(101, "/crypto-alert configure"))

	if code := postTelegramUpdate(t, adapter, telegramCallback(102, 7, "symbol:BTCUSDT")); code != http.StatusOK {
		t.Fatalf("unexpected status %d", code)
	}

	payload := lastPayloadForMethod(t, calls, "editMessageText")
	if got, want := payload["text"], "Select symbols for alerts.\nSelected: BTCUSDT"; got != want {
		t.Fatalf("unexpected symbol callback text: got %q want %q", got, want)
	}
	texts := keyboardTexts(t, payload)
	if strings.Join(texts, ",") != "✓ BTCUSDT,Next,Cancel" {
		t.Fatalf("unexpected selected symbol keyboard: %v", texts)
	}
	if commands.updates != 1 {
		t.Fatalf("expected one session update, got %d", commands.updates)
	}
}

func TestNextShowsIntervalControlsForSameSession(t *testing.T) {
	var calls []telegramAPICall
	events := &fakeEvents{}
	commands := &fakeCommands{}
	adapter := newAdapter(t, newTelegramAPIRecorder(t, &calls, 7), events, commands)
	postTelegramUpdate(t, adapter, telegramMessage(103, "/crypto-alert configure"))
	postTelegramUpdate(t, adapter, telegramCallback(104, 7, "symbol:BTCUSDT"))

	if code := postTelegramUpdate(t, adapter, telegramCallback(105, 7, "next:intervals")); code != http.StatusOK {
		t.Fatalf("unexpected status %d", code)
	}

	payload := lastPayloadForMethod(t, calls, "editMessageText")
	if got, want := payload["text"], "Select intervals for alerts.\nSelected: none"; got != want {
		t.Fatalf("unexpected interval screen text: got %q want %q", got, want)
	}
	texts := keyboardTexts(t, payload)
	if strings.Join(texts, ",") != "1h,Back,Save,Cancel" {
		t.Fatalf("unexpected interval keyboard: %v", texts)
	}
	if commands.updates != 1 {
		t.Fatalf("navigation should not update session, got %d updates", commands.updates)
	}
}

func TestIntervalCallbackUpdatesSessionAndKeepsIntervalScreen(t *testing.T) {
	var calls []telegramAPICall
	events := &fakeEvents{}
	commands := &fakeCommands{}
	adapter := newAdapter(t, newTelegramAPIRecorder(t, &calls, 7), events, commands)
	postTelegramUpdate(t, adapter, telegramMessage(106, "/crypto-alert configure"))
	postTelegramUpdate(t, adapter, telegramCallback(107, 7, "next:intervals"))

	if code := postTelegramUpdate(t, adapter, telegramCallback(108, 7, "interval:1h")); code != http.StatusOK {
		t.Fatalf("unexpected status %d", code)
	}

	payload := lastPayloadForMethod(t, calls, "editMessageText")
	if got, want := payload["text"], "Select intervals for alerts.\nSelected: 1h"; got != want {
		t.Fatalf("unexpected selected interval text: got %q want %q", got, want)
	}
	texts := keyboardTexts(t, payload)
	if strings.Join(texts, ",") != "✓ 1h,Back,Save,Cancel" {
		t.Fatalf("unexpected selected interval keyboard: %v", texts)
	}
	if commands.updates != 1 {
		t.Fatalf("expected one session update, got %d", commands.updates)
	}
}

func TestBackReturnsToSymbolsWithoutLosingSelections(t *testing.T) {
	var calls []telegramAPICall
	events := &fakeEvents{}
	commands := &fakeCommands{}
	adapter := newAdapter(t, newTelegramAPIRecorder(t, &calls, 7), events, commands)
	postTelegramUpdate(t, adapter, telegramMessage(109, "/crypto-alert configure"))
	postTelegramUpdate(t, adapter, telegramCallback(110, 7, "symbol:BTCUSDT"))
	postTelegramUpdate(t, adapter, telegramCallback(111, 7, "next:intervals"))
	postTelegramUpdate(t, adapter, telegramCallback(112, 7, "interval:1h"))

	if code := postTelegramUpdate(t, adapter, telegramCallback(113, 7, "back:symbols")); code != http.StatusOK {
		t.Fatalf("unexpected status %d", code)
	}

	payload := lastPayloadForMethod(t, calls, "editMessageText")
	if got, want := payload["text"], "Select symbols for alerts.\nSelected: BTCUSDT"; got != want {
		t.Fatalf("unexpected back text: got %q want %q", got, want)
	}
	texts := keyboardTexts(t, payload)
	if strings.Join(texts, ",") != "✓ BTCUSDT,Next,Cancel" {
		t.Fatalf("unexpected back keyboard: %v", texts)
	}
}

func TestSaveAndCancelRouteThroughCommandService(t *testing.T) {
	t.Run("save", func(t *testing.T) {
		var calls []telegramAPICall
		events := &fakeEvents{}
		commands := &fakeCommands{}
		adapter := newAdapter(t, newTelegramAPIRecorder(t, &calls, 7), events, commands)
		postTelegramUpdate(t, adapter, telegramMessage(114, "/crypto-alert configure"))

		if code := postTelegramUpdate(t, adapter, telegramCallback(115, 7, "save")); code != http.StatusOK {
			t.Fatalf("unexpected status %d", code)
		}
		if commands.action != chat.ActionSave {
			t.Fatalf("expected save action, got %s", commands.action)
		}
		payload := lastPayloadForMethod(t, calls, "sendMessage")
		if payload["text"] != "ok" {
			t.Fatalf("unexpected save response payload: %+v", payload)
		}
		answerPayload := lastPayloadForMethod(t, calls, "answerCallbackQuery")
		if answerPayload["callback_query_id"] != "callback-115" {
			t.Fatalf("unexpected save callback answer payload: %+v", answerPayload)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		var calls []telegramAPICall
		events := &fakeEvents{}
		commands := &fakeCommands{}
		adapter := newAdapter(t, newTelegramAPIRecorder(t, &calls, 7), events, commands)
		postTelegramUpdate(t, adapter, telegramMessage(116, "/crypto-alert configure"))

		if code := postTelegramUpdate(t, adapter, telegramCallback(117, 7, "cancel")); code != http.StatusOK {
			t.Fatalf("unexpected status %d", code)
		}
		if commands.action != chat.ActionCancel {
			t.Fatalf("expected cancel action, got %s", commands.action)
		}
		payload := lastPayloadForMethod(t, calls, "sendMessage")
		if payload["text"] != "ok" {
			t.Fatalf("unexpected cancel response payload: %+v", payload)
		}
		answerPayload := lastPayloadForMethod(t, calls, "answerCallbackQuery")
		if answerPayload["callback_query_id"] != "callback-117" {
			t.Fatalf("unexpected cancel callback answer payload: %+v", answerPayload)
		}
	})
}

func TestUnauthorizedCallbackUserIsRejected(t *testing.T) {
	var calls []telegramAPICall
	events := &fakeEvents{}
	commands := &fakeCommands{}
	adapter := newAdapter(t, newTelegramAPIRecorder(t, &calls, 7), events, commands)
	postTelegramUpdate(t, adapter, telegramMessage(118, "/crypto-alert configure"))

	if code := postTelegramUpdate(t, adapter, telegramCallback(119, 8, "symbol:BTCUSDT")); code != http.StatusOK {
		t.Fatalf("unexpected status %d", code)
	}
	if !events.failed {
		t.Fatal("expected unauthorized callback to be marked failed")
	}
	if commands.updates != 0 {
		t.Fatalf("unauthorized callback should not update session, got %d updates", commands.updates)
	}
}
