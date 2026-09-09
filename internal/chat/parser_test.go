package chat

import "testing"

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input     string
		action    CommandAction
		arguments []string
	}{
		{"/crypto-alert configure BTCUSDT", ActionConfigure, []string{"BTCUSDT"}},
		{"configure", ActionConfigure, nil},
		{"/show", ActionShow, nil},
		{"/crypto-alert", ActionHelp, nil},
	}
	for _, tt := range tests {
		action, arguments, err := ParseCommand(tt.input)
		if err != nil || action != tt.action || len(arguments) != len(tt.arguments) {
			t.Fatalf("parse %q = %q %v %v", tt.input, action, arguments, err)
		}
	}
	if _, _, err := ParseCommand("/unknown"); err == nil {
		t.Fatal("expected unknown command error")
	}
}
