package notification

import (
	"bytes"
	"context"
	"crypto-price-alert/internal/domain"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDryRunNotifierLogsWithoutSending(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	notifier := NewDryRunNotifier("telegram", logger)

	message := domain.Message{Title: "hello"}
	if err := notifier.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "notification dry run") || !strings.Contains(logs.String(), "hello") {
		t.Fatalf("unexpected dry-run log: %s", logs.String())
	}
}

type notifierRoundTripper func(*http.Request) (*http.Response, error)

func (f notifierRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func notifierResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestTelegramNotifierSendsMessage(t *testing.T) {
	var gotBody string
	client := &http.Client{Transport: notifierRoundTripper(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		return notifierResponse(http.StatusOK, `{"ok":true}`), nil
	})}
	notifier, err := NewTelegramNotifier("token", "chat", "http://telegram.test", client, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := notifier.SendMessage(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"chat_id":"chat"`) || !strings.Contains(gotBody, `"text":"hello"`) {
		t.Fatalf("unexpected body: %s", gotBody)
	}
}

func TestTelegramNotifierSendsToTargetChat(t *testing.T) {
	var gotBody string
	client := &http.Client{Transport: notifierRoundTripper(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		return notifierResponse(http.StatusOK, `{"ok":true}`), nil
	})}
	notifier, err := NewTelegramNotifier("token", "legacy-chat", "http://telegram.test", client, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	err = notifier.SendToTarget(context.Background(), domain.AlertTarget{Provider: domain.ChatProviderTelegram, ExternalChatID: "target-chat"}, domain.Message{Title: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"chat_id":"target-chat"`) {
		t.Fatalf("message was sent to the wrong target: %s", gotBody)
	}
}

func TestSlackNotifierRetriesTransientError(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: notifierRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return notifierResponse(http.StatusBadGateway, "temporary"), nil
		}
		return notifierResponse(http.StatusOK, "ok"), nil
	})}
	notifier, err := NewSlackNotifier("http://slack.test", client, 2, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := notifier.SendMessage(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
}

func TestSlackBotNotifierSendsToTargetChannel(t *testing.T) {
	var bodies []string
	client := &http.Client{Transport: notifierRoundTripper(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		return notifierResponse(http.StatusOK, `{"ok":true}`), nil
	})}

	notifier, err := NewSlackBotNotifier("xoxb-token", "http://slack.test/api", client, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"channel-a", "channel-b"} {
		err = notifier.SendToTarget(context.Background(), domain.AlertTarget{Provider: domain.ChatProviderSlack, ExternalChatID: target}, domain.Message{Title: "hello"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(bodies[0], `"channel":"channel-a"`) || !strings.Contains(bodies[1], `"channel":"channel-b"`) {
		t.Fatalf("messages were routed to the wrong channels: %v", bodies)
	}
}

func TestNotifierConstructorsValidateSettings(t *testing.T) {
	if _, err := NewTelegramNotifier("", "chat", "", nil, 1, time.Millisecond); err == nil {
		t.Fatal("expected Telegram validation error")
	}
	if _, err := NewSlackNotifier("", nil, 1, time.Millisecond); err == nil {
		t.Fatal("expected Slack validation error")
	}
}
