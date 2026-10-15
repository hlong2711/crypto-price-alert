package chat

import (
	"context"
	"crypto-price-alert/internal/domain"
	"strings"
	"testing"
)

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

func TestFundingCommandScopeAndParsing(t *testing.T) {
	for _, text := range []string{"/funding", "/funding 10", "/funding@MyBot 20"} {
		a, args, err := ParseCommand(text)
		if err != nil || a != ActionFunding || a.IsMutation() {
			t.Fatal(a, args, err)
		}
	}
	for _, provider := range []domain.ChatProvider{domain.ChatProviderTelegram, domain.ChatProviderSlack} {
		definitions := PublicCommandDefinitionsFor(provider)
		found := false
		for _, d := range definitions {
			found = found || d.Action == ActionFunding
		}
		if found != (provider == domain.ChatProviderTelegram) {
			t.Fatal("wrong command scope", provider)
		}
		s, _, _, target := newChatService(t)
		target.Provider = provider
		help, err := s.Handle(context.Background(), Command{Target: target, ActorUserID: "member", Action: ActionHelp})
		if err != nil || strings.Contains(help, "funding") != found {
			t.Fatal(help, err)
		}
	}
}
