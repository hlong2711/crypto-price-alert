package chat

import "testing"

func TestPublicCommandDefinitionsExcludeInternalSessionActions(t *testing.T) {
	definitions := PublicCommandDefinitions()
	if len(definitions) != 6 {
		t.Fatalf("got %d public commands, want 6", len(definitions))
	}
	for _, definition := range definitions {
		if definition.Action == ActionSave || definition.Action == ActionCancel {
			t.Fatalf("internal action %q exposed as public command", definition.Action)
		}
	}
}
