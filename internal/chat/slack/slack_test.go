package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"crypto-price-alert/internal/chat"
	"crypto-price-alert/internal/domain"
	"github.com/labstack/echo/v4"
)

func TestVerifySignature(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	body := []byte("command=%2Fcrypto-alert&text=help")
	timestamp := "1700000000"
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte("v0:" + timestamp + ":" + string(body)))
	signature := "v0=" + encodeHex(mac.Sum(nil))

	if err := VerifySignature("secret", timestamp, signature, body, now, 5*time.Minute); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := VerifySignature("wrong", timestamp, signature, body, now, 5*time.Minute); err == nil {
		t.Fatal("expected invalid signature")
	}
	if err := VerifySignature("secret", "1699990000", signature, body, now, 5*time.Minute); err == nil {
		t.Fatal("expected stale timestamp")
	}
}

func TestSlashCommandWebhookAcknowledgesAndDispatches(t *testing.T) {
	transport := &slackTransport{}
	client, err := NewAPIClient("xoxb-test", "https://slack.test/api", &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	events := &fakeEvents{claimed: true, processed: make(chan struct{})}
	commands := &fakeCommands{handled: make(chan chat.Command, 1)}
	adapter, err := NewAdapter(client, fakeTargets{}, events, commands, chat.NewMemorySessionStore(), "secret", "tenant", "app", "team", []string{"BTCUSDT"}, []domain.Interval{domain.Interval1H})
	if err != nil {
		t.Fatal(err)
	}
	adapter.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	body := []byte(url.Values{"token": {"legacy"}, "api_app_id": {"app"}, "team_id": {"team"}, "channel_id": {"C1"}, "channel_name": {"alerts"}, "user_id": {"U1"}, "command": {"/crypto-alert"}, "text": {"help"}, "response_url": {"https://slack.test/response"}, "trigger_id": {"T1"}}.Encode())
	recorder := echo.New().NewContext(newRequest(body, signature("secret", "1700000000", body)), newRecorder())
	if err := adapter.SlashCommandWebhook(recorder); err != nil {
		t.Fatal(err)
	}
	if recorder.Response().Status != http.StatusOK {
		t.Fatalf("expected acknowledgment, got %d", recorder.Response().Status)
	}
	select {
	case command := <-commands.handled:
		if command.Action != chat.ActionHelp || command.Target.ExternalChatID != "C1" {
			t.Fatalf("unexpected command: %+v", command)
		}
	case <-time.After(time.Second):
		t.Fatal("command was not dispatched")
	}
	select {
	case <-events.processed:
	case <-time.After(time.Second):
		t.Fatal("event was not marked processed")
	}
}

func TestConfigurationBlocksUseSessionScopedValues(t *testing.T) {
	blocks := ConfigurationBlocks("session-1", []string{"BTCUSDT"}, []domain.Interval{domain.Interval4H})
	data, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"session-1|symbol|BTCUSDT", "session-1|interval|4h", "crypto_alert_save", "crypto_alert_cancel"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Block Kit payload missing %q: %s", expected, text)
		}
	}
}

func signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("v0:" + timestamp + ":" + string(body)))
	return "v0=" + encodeHex(mac.Sum(nil))
}

func encodeHex(value []byte) string {
	const hex = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for i, b := range value {
		result[i*2] = hex[b>>4]
		result[i*2+1] = hex[b&15]
	}
	return string(result)
}

func newRequest(body []byte, signature string) *http.Request {
	request, _ := http.NewRequest(http.MethodPost, "/slack", strings.NewReader(string(body)))
	request.Header.Set("X-Slack-Request-Timestamp", "1700000000")
	request.Header.Set("X-Slack-Signature", signature)
	return request
}

func newRecorder() *responseRecorder {
	return &responseRecorder{header: make(http.Header)}
}

type responseRecorder struct {
	code   int
	header http.Header
	body   strings.Builder
}

func (r *responseRecorder) Header() http.Header  { return r.header }
func (r *responseRecorder) WriteHeader(code int) { r.code = code }
func (r *responseRecorder) Write(value []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.body.Write(value)
}

func (r *responseRecorder) StatusCode() int { return r.code }

type slackTransport struct{}

func (*slackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body string
	if strings.HasSuffix(request.URL.Path, "conversations.info") {
		body = `{"ok":true,"channel":{"id":"C1","creator":"U1"}}`
	} else {
		body = `{"ok":true}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

type fakeEvents struct {
	claimed   bool
	processed chan struct{}
}

func (f *fakeEvents) ClaimInboundEvent(context.Context, domain.InboundEvent) (bool, error) {
	return f.claimed, nil
}
func (f *fakeEvents) MarkInboundEventProcessed(context.Context, domain.ChatProvider, string, time.Time) error {
	close(f.processed)
	return nil
}
func (f *fakeEvents) MarkInboundEventFailed(context.Context, domain.ChatProvider, string, string) error {
	return nil
}

type fakeTargets struct{}

func (fakeTargets) FindOrCreateTarget(context.Context, domain.AlertTarget) (domain.AlertTarget, error) {
	return domain.AlertTarget{ID: "target-1", Provider: domain.ChatProviderSlack, TenantID: "tenant", ExternalChatID: "C1", CreatorUserID: "U1"}, nil
}
func (fakeTargets) GetTarget(context.Context, domain.ChatProvider, string, string) (domain.AlertTarget, error) {
	return domain.AlertTarget{ID: "target-1", Provider: domain.ChatProviderSlack, TenantID: "tenant", ExternalChatID: "C1", CreatorUserID: "U1"}, nil
}

type fakeCommands struct {
	mu      sync.Mutex
	handled chan chat.Command
}

func (f *fakeCommands) Handle(_ context.Context, command chat.Command) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handled <- command
	return "help, configure, show, enable, pause, test", nil
}
func (*fakeCommands) UpdateSession(context.Context, chat.Command, []string, []domain.Interval) error {
	return nil
}
