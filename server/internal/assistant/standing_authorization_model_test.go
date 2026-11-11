package assistant

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestStandingAuthorizationConfiguredModelAndFallback(t *testing.T) {
	for _, available := range []bool{true, false} {
		name := "configured_model"
		if !available {
			name = "unavailable_model_reaches_user_and_briefing"
		}
		t.Run(name, func(t *testing.T) {
			database := taskRecoveryDatabase(t)
			e := newRouteEnv(t, database, false)
			t.Cleanup(func() { settleService(t, e.s) })
			e.f.ownModels = true
			saveDispatchAuthorizations(t, database, routeStandingGrant())
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body struct{ Model string }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "configured-task-judge" {
					t.Errorf("purpose model selection: %+v %v", body, err)
				}
				if !available {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				reply := `{"within_task":true,"complete":true,"proceed":true,"actions":["merge","verify"],"answers":{},"reason":"Merge the task PR and verify locally."}`
				_ = json.NewEncoder(w).Encode(map[string]any{"content": []any{map[string]any{"type": "text", "text": reply}}})
			}))
			defer srv.Close()
			key, err := e.s.sealKey("synthetic-test-key")
			if err != nil {
				t.Fatal(err)
			}
			putLaunchers(t, e.s, storedLauncher{ManagedLauncher: proto.ManagedLauncher{Name: "task-judge", Kind: "api", API: "messages", BaseURL: srv.URL, Model: "provider-default"}, KeySealed: key})
			setModel(t, e.s, "agent.delegate", ModelBinding{Provider: "launcher.task-judge", Model: "configured-task-judge"})
			ix := proto.Interaction{ID: "configured-judge-card", MachineID: "mac", SessionID: "worker-1", Kind: proto.IxPlan,
				ToolName: "ExitPlanMode", Plan: "Merge the pull request, then run local tests.", CreatedAt: decisionNow(), ExpiresAt: time.Now().Add(time.Hour)}
			p := &pending{ix: ix, done: make(chan proto.Resolution, 1)}
			putPending(e.s, p)
			e.s.record(ix)
			e.s.announce(ix)
			if calls.Load() == 0 {
				t.Fatal("standing judge did not use the configured purpose provider")
			}
			if available {
				select {
				case answer := <-p.done:
					if answer.Approve == nil || !*answer.Approve || answer.Mode != "default" {
						t.Fatalf("plan resolution: %+v", answer)
					}
				default:
					t.Fatal("configured model did not approve the eligible card")
				}
				return
			}
			if len(decisionsOf(t, database, e.todo.ID)) != 0 {
				t.Fatal("unavailable model created an automatic decision")
			}
			select {
			case <-p.done:
				t.Fatal("unavailable model automatically answered the card")
			default:
			}
			e.s.askTick(time.Now().Add(2 * askHoldFor))
			waitFor(t, "manual fallback notification", func() bool { return e.pushesOf(ix.ID) == 1 })
			facts := e.s.briefFactsAt(t.Context(), time.Now().Add(time.Second), "")
			if facts.Confirmations.UserNotified != 1 || facts.Confirmations.AutoAuthorized != 0 || len(facts.Confirmations.Sources) != 1 || facts.Confirmations.Sources[0].Kind != proto.IxPlan {
				t.Fatalf("manual fallback briefing counts: %+v", facts.Confirmations)
			}
			facts.l = i18n.Lang("en")
			facts.ConfirmationLines = e.s.confirmationLines(facts.l, facts.Confirmations)
			_, plain, _ := e.s.briefCard(facts, "Morning briefing", "")
			if !strings.Contains(plain, "1 confirmation cards sent to you") {
				t.Fatalf("manual fallback absent from briefing: %s", plain)
			}
			t.Logf("MODEL_UNAVAILABLE_BRIEFING\n%s", plain)
		})
	}
}

func TestStandingAuthorizationForcePushNeedsDeletionGrant(t *testing.T) {
	for _, plan := range []string{"Force-push the rebased branch to replace the remote history.", "git push --force-with-lease origin feature", "强推变基后的分支覆盖远端历史。"} { // i18n-ignore: synthetic card
		ix := proto.Interaction{Kind: proto.IxPlan, Plan: plan}
		m := &standingJudgeTestModel{reply: standingJudgeReply(t, []string{"push"}, nil)}
		a, err := assessStandingWith(t.Context(), m, ix, "Update the task branch.", "/code/app")
		if err != nil {
			t.Fatal(err)
		}
		grant := routeStandingGrant()
		grant.Actions = []string{"push"}
		if standingAssessmentAllowed(grant, a, ix) {
			t.Fatal("ordinary push authorization allowed remote history replacement")
		}
		grant.Actions = append(grant.Actions, "delete")
		grant.Reversible = false
		if standingAssessmentAllowed(grant, a, ix) {
			t.Fatal("irreversible history replacement was authorized")
		}
		grant.Reversible = true
		if !standingAssessmentAllowed(grant, a, ix) {
			t.Fatal("explicit reversible history replacement was rejected")
		}
	}
}
