package slack

import (
	"fmt"

	"crypto-price-alert/internal/domain"
)

// ConfigurationBlocks renders allowed symbols and intervals as Block Kit controls.
func ConfigurationBlocks(sessionID string, symbols []string, intervals []domain.Interval) []Block {
	blocks := []Block{{
		Type: "section",
		Text: &TextObject{Type: "mrkdwn", Text: "Select symbols and intervals, then save."},
	}}

	for _, symbol := range symbols {
		blocks = append(blocks, Block{Type: "actions", Elements: []BlockElement{{
			Type:     "button",
			ActionID: "crypto_alert_symbol",
			Text:     &TextObject{Type: "plain_text", Text: symbol},
			Value:    fmt.Sprintf("%s|symbol|%s", sessionID, symbol),
		}}})
	}

	for _, interval := range intervals {
		value := string(interval)
		blocks = append(blocks, Block{Type: "actions", Elements: []BlockElement{{
			Type:     "button",
			ActionID: "crypto_alert_interval",
			Text:     &TextObject{Type: "plain_text", Text: value},
			Value:    fmt.Sprintf("%s|interval|%s", sessionID, value),
		}}})
	}

	blocks = append(blocks, Block{Type: "actions", Elements: []BlockElement{
		{
			Type:     "button",
			ActionID: "crypto_alert_save",
			Text:     &TextObject{Type: "plain_text", Text: "Save"},
			Value:    sessionID,
		},
		{
			Type:     "button",
			ActionID: "crypto_alert_cancel",
			Text:     &TextObject{Type: "plain_text", Text: "Cancel"},
			Value:    sessionID,
		},
	}})

	return blocks
}
