package assistant

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/nwverse/persona/server/internal/apierr"
)

// Inbox handling settings (docs/assistant/inbox.md "Inbox handling settings", #4524): the Inbox's own switches for what
// it does with an item past triage, kept under inboxSettingsKey beside its policies and folders. They are one object,
// read whole and changed field by field (GET/PUT /api/v1/inbox/settings), so the page that gathers the Inbox's handling
// (#4501) adds its switches here.

// inboxSettingsKey is where the handling settings are kept.
const inboxSettingsKey = "inbox_settings"

// InboxSettings are the Inbox's handling settings, as kept and as GET /api/v1/inbox/settings answers them.
type InboxSettings struct {
	// ForwardToTasks: new messages from other people that the AI twin's judge links to a supervised task reach the task's
	// owner, the conversation supervising it (supervise_inbox.go). Off by default: each one is a turn of that
	// conversation's model.
	ForwardToTasks bool `json:"forward_to_tasks"`
}

// InboxSettingsChange is a PUT's body: what it names changes, the rest stays.
type InboxSettingsChange struct {
	ForwardToTasks *bool `json:"forward_to_tasks"`
}

// loadInboxSettings reads the handling settings; false when the database could not be read. While they were never
// saved, forward_to_tasks starts from where it was kept before it moved here (the assistant's inbox_signals, #4381): an
// "on" found there is saved here once and said in the log, so nobody who turned it on finds it off. That read goes after
// a release (#2656).
func (s *Service) loadInboxSettings(ctx context.Context) bool {
	st := s.store()
	if st == nil {
		return true
	}
	var cfg InboxSettings
	found, err := st.Setting(ctx, inboxSettingsKey, &cfg)
	if err != nil {
		s.logf("inbox: settings: %v", err)
		return false
	}
	if !found {
		if old := s.settings(ctx).InboxSignals; old != nil && *old {
			cfg.ForwardToTasks = true
			if err := st.PutSetting(ctx, inboxSettingsKey, cfg); err != nil {
				s.logf("inbox: carry assistant.inbox_signals over to inbox.forward_to_tasks: %v (tried again on the next start)", err)
			} else {
				s.logf("inbox: assistant.inbox_signals is now inbox.forward_to_tasks; it was on and stays on")
			}
		}
	}
	b := &s.inbox
	b.mu.Lock()
	b.settingsCfg = cfg
	b.mu.Unlock()
	return true
}

// setInboxSettings changes what c names, keeping it before memory changes.
func (s *Service) setInboxSettings(ctx context.Context, c InboxSettingsChange) (InboxSettings, error) {
	b := &s.inbox
	b.settingsCfgMu.Lock()
	defer b.settingsCfgMu.Unlock()
	b.mu.Lock()
	cfg := b.settingsCfg
	b.mu.Unlock()
	if c.ForwardToTasks != nil {
		cfg.ForwardToTasks = *c.ForwardToTasks
	}
	if st := s.store(); st != nil {
		if err := st.PutSetting(ctx, inboxSettingsKey, cfg); err != nil {
			return cfg, err
		}
	}
	b.mu.Lock()
	b.settingsCfg = cfg
	b.mu.Unlock()
	return cfg, nil
}

// inboxSettingsNow are the handling settings in effect (the defaults until the inbox has read them).
func (s *Service) inboxSettingsNow() InboxSettings {
	b := &s.inbox
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.settingsCfg
}

// inboxForwardOn: inbox.forward_to_tasks is on (off by default).
func (s *Service) inboxForwardOn() bool { return s.inboxSettingsNow().ForwardToTasks }

func (s *Service) inboxSettingsRoutes() {
	s.handle("GET /api/v1/inbox/settings", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		apierr.WriteJSON(w, s.inboxSettingsNow())
	})
	// {"forward_to_tasks"?}: what is given changes.
	s.handle("PUT /api/v1/inbox/settings", func(w http.ResponseWriter, r *http.Request) {
		if !s.inboxUp(w, r) {
			return
		}
		var in InboxSettingsChange
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			apierr.Write(w, http.StatusBadRequest, err)
			return
		}
		cfg, err := s.setInboxSettings(r.Context(), in)
		if err != nil {
			apierr.Write(w, http.StatusInternalServerError, err)
			return
		}
		apierr.WriteJSON(w, cfg)
	})
}
