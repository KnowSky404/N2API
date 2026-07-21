package systemevent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildEventCarriesRequestAndIntentContext(t *testing.T) {
	ctx := WithRequestContext(context.Background(), RequestContext{
		CorrelationID: "request-42", SourceIP: "192.0.2.10", HTTPMethod: "POST", RoutePattern: "POST /api/admin/keys",
		Actor: Actor{Type: ActorAdmin, ID: 7, Name: "admin"},
	})
	ctx = WithIntent(ctx, EventIntent{Category: CategoryAudit, Severity: SeverityInfo, Action: ActionAPIKeyCreated, Outcome: OutcomeSuccess})
	intent, ok := IntentFromContext(ctx)
	if !ok {
		t.Fatal("intent missing")
	}
	event := BuildEvent(ctx, intent, Target{Type: "api_key", ID: "9", Name: "laptop"}, time.Unix(100, 0), 12*time.Millisecond)
	if event.CorrelationID != "request-42" || event.Actor.ID != 7 || event.Target.ID != "9" || event.DurationMS != 12 {
		t.Fatalf("event = %+v", event)
	}
	if err := ValidateEvent(event); err != nil {
		t.Fatalf("ValidateEvent returned error: %v", err)
	}
}

func TestValidateEventRejectsSecretsUnknownActionsAndOversizedMetadata(t *testing.T) {
	base := Event{OccurredAt: time.Now(), Category: CategoryAudit, Severity: SeverityInfo, Action: ActionAPIKeyCreated,
		Outcome: OutcomeSuccess, Actor: Actor{Type: ActorAdmin}, CorrelationID: "request-1", Metadata: map[string]any{}}
	tests := []Event{
		func() Event { value := base; value.Action = "unregistered.action"; return value }(),
		func() Event { value := base; value.Message = "line one\nline two"; return value }(),
		func() Event {
			value := base
			value.Metadata = map[string]any{"nested": map[string]any{"refresh_token": "canary"}}
			return value
		}(),
		func() Event {
			value := base
			value.Metadata = map[string]any{"nested": map[string]string{"access_token": "canary"}}
			return value
		}(),
		func() Event {
			value := base
			value.Metadata = map[string]any{"value": strings.Repeat("x", MaxMetadataEncodedSize)}
			return value
		}(),
	}
	for i, event := range tests {
		if err := ValidateEvent(event); err == nil {
			t.Fatalf("case %d returned nil error", i)
		}
	}
}

func TestSafeMetadataRequiresExplicitAllowlist(t *testing.T) {
	metadata, err := SafeMetadata(map[string]any{"changed_fields": []string{"name"}}, "changed_fields")
	if err != nil || len(metadata) != 1 {
		t.Fatalf("SafeMetadata = %#v, %v", metadata, err)
	}
	if _, err := SafeMetadata(map[string]any{"unexpected": true}, "changed_fields"); err == nil {
		t.Fatal("unexpected key accepted")
	}
	if _, err := SafeMetadata(map[string]any{"access_token": "canary"}, "access_token"); err == nil {
		t.Fatal("secret key accepted")
	}
}

func TestNormalizeCorrelationIDRejectsUnsafeIncomingValue(t *testing.T) {
	if got := NormalizeCorrelationID("known.request-1"); got != "known.request-1" {
		t.Fatalf("valid ID replaced with %q", got)
	}
	if got := NormalizeCorrelationID("bad value\n"); got == "bad value\n" || !ValidCorrelationID(got) {
		t.Fatalf("invalid replacement = %q", got)
	}
}

func TestKnownActionCatalogProducesValidEvents(t *testing.T) {
	for action := range knownActions {
		event := Event{
			OccurredAt: time.Now().UTC(), Category: CategoryAudit, Severity: SeverityInfo,
			Action: action, Outcome: OutcomeSuccess, Actor: Actor{Type: ActorSystem},
			CorrelationID: "catalog-test", Metadata: map[string]any{},
		}
		if err := ValidateEvent(event); err != nil {
			t.Errorf("action %q failed validation: %v", action, err)
		}
	}
}

func TestSessionRevocationActionsAreKnown(t *testing.T) {
	for _, action := range []Action{ActionAuthSessionRevoked, ActionAuthSessionsRevokedOthers} {
		if !IsKnownAction(action) {
			t.Fatalf("action %q is not registered", action)
		}
	}
}
