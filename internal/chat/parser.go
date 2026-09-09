package chat

import (
	"fmt"
	"strings"
)

var commandActions = map[string]CommandAction{
	"help":      ActionHelp,
	"configure": ActionConfigure,
	"config":    ActionConfigure,
	"show":      ActionShow,
	"enable":    ActionEnable,
	"pause":     ActionPause,
	"test":      ActionTest,
	"save":      ActionSave,
	"cancel":    ActionCancel,
}

// ParseCommand normalizes plain text and slash-command input. Platform adapters
// are responsible for extracting the target, actor, and event metadata.
func ParseCommand(input string) (CommandAction, []string, error) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) == 0 {
		return "", nil, fmt.Errorf("command is required")
	}
	name := strings.TrimPrefix(strings.ToLower(fields[0]), "/")
	if name == "crypto-alert" {
		fields = fields[1:]
		if len(fields) == 0 {
			return ActionHelp, nil, nil
		}
		name = strings.ToLower(fields[0])
	}
	action, ok := commandActions[name]
	if !ok {
		return "", nil, fmt.Errorf("unknown command %q", name)
	}
	return action, fields[1:], nil
}
