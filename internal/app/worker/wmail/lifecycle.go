package wmail

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// ExecutionKey excludes checkpoints and policy budgets from client identity.
func ExecutionKey(data *models.AddWorkerEmail) [32]byte {
	type credential struct{ Access, Refresh string }
	tokenKey := func(t *oauth2.Token) credential {
		if t == nil {
			return credential{}
		}
		return credential{t.AccessToken, t.RefreshToken}
	}
	key := struct {
		ID, User                       uuid.UUID
		Org                            *uuid.UUID
		Provider                       models.InboxProvider
		Email, First, Last, GraphUser  string
		Brokered, ImapSync, SaveToSent bool
		Token                          credential
		SMTP, IMAP                     *models.Service
		Skip                           []string
	}{ID: data.ID, User: data.UserID, Org: data.OrganizationID, Provider: data.Type,
		Email: data.Email, First: data.FirstName, Last: data.LastName, Brokered: data.Brokered,
		ImapSync: data.Type == models.InboxProviderSMTPIMAP && data.ImapSync, SaveToSent: data.Type == models.InboxProviderSMTPIMAP && data.SavesSentCopy()}
	if data.Type == models.InboxProviderGoogle && data.Google != nil && !data.Brokered {
		key.Token = tokenKey(data.Google.Token)
	}
	if data.Type == models.InboxProviderOutlook && data.Graph != nil {
		key.GraphUser = data.Graph.User
		if !data.Brokered {
			key.Token = tokenKey(data.Graph.Token)
		}
	}
	if data.Type == models.InboxProviderSMTPIMAP && data.SmtpImap != nil && data.SmtpImap.Credentials != nil {
		key.SMTP, key.IMAP = data.SmtpImap.Credentials.SMTP, data.SmtpImap.Credentials.IMAP
	}
	if data.Sync != nil && data.Type == models.InboxProviderSMTPIMAP {
		for _, folder := range data.Sync.Policy.SkipFolders {
			if folder = strings.ToLower(strings.TrimSpace(folder)); folder != "" {
				key.Skip = append(key.Skip, folder)
			}
		}
		slices.Sort(key.Skip)
		key.Skip = slices.Compact(key.Skip)
	}
	b, _ := json.Marshal(key)
	return sha256.Sum256(b)
}

func (w *WMail) rememberExecution(data *models.AddWorkerEmail) {
	// Own the token pointers; ADD_EMAIL payloads may be reused by a publisher.
	copyData := *data
	if data.Google != nil {
		g := *data.Google
		copyData.Google = &g
	}
	if data.Graph != nil {
		g := *data.Graph
		copyData.Graph = &g
	}
	w.executionData = &copyData
	w.initialExecutionKey = ExecutionKey(data)
	w.executionKeys = map[[32]byte]struct{}{w.initialExecutionKey: {}}
}

func (w *WMail) SameExecution(data *models.AddWorkerEmail) bool {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	_, ok := w.executionKeys[ExecutionKey(data)]
	return ok && !w.stopped
}

// BeginExecution rejects stale references and lets replacements drain admitted work.
func (w *WMail) BeginExecution() (func(), bool) {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if w.stopped {
		return nil, false
	}
	w.active.Add(1)
	return w.active.Done, true
}

// Stop cancels scheduling and closes the provider without waiting on its caller.
func (w *WMail) Stop() {
	w.lifecycleMu.Lock()
	if w.stopped {
		w.lifecycleMu.Unlock()
		return
	}
	w.stopped = true
	w.stopDone = make(chan struct{})
	done := w.stopDone
	w.lifecycleMu.Unlock()
	defer close(done)
	if w.Cancel != nil {
		w.Cancel()
	}
	if w.SmtpImapData != nil && w.SmtpImapData.ImapClient != nil {
		if c, ok := w.SmtpImapData.ImapClient.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
}

// Drain is called outside mailbox execution, after Stop prevents new work.
func (w *WMail) Drain() {
	w.lifecycleMu.Lock()
	done := w.stopDone
	w.lifecycleMu.Unlock()
	if done != nil {
		<-done
	}
	w.active.Wait()
}

// ResumeInto uses the drained live state, which can be newer than ADD_EMAIL.
func (w *WMail) ResumeInto(data *models.AddWorkerEmail) {
	if w.UserID != data.UserID || w.EmailType != data.Type {
		return
	}
	if w.tracker != nil {
		if data.Sync == nil {
			data.Sync = &models.AddWorkerEmailSyncData{}
		} else {
			s := *data.Sync
			data.Sync = &s
		}
		state := w.tracker.state
		state.BackfillCursor.Folders = make(map[string]models.SyncFolderCursor, len(w.tracker.state.BackfillCursor.Folders))
		for k, v := range w.tracker.state.BackfillCursor.Folders {
			state.BackfillCursor.Folders[k] = v
		}
		data.Sync.State = &state
	}
	if w.GoogleData != nil && data.Google != nil {
		g := *data.Google
		g.LastHistoryID = w.GoogleData.LastHistoryID
		data.Google = &g
	}
	if w.GraphData != nil && data.Graph != nil {
		g := *data.Graph
		g.DeltaLinks = cloneStringMap(w.GraphData.Client.DeltaLinks)
		data.Graph = &g
	}
	if w.SmtpImapData != nil && data.SmtpImap != nil {
		s := *data.SmtpImap
		s.Mailboxes = nil
		for _, box := range w.SmtpImapData.Mailboxes {
			if box != nil {
				s.Mailboxes = append(s.Mailboxes, *box)
			}
		}
		data.SmtpImap = &s
	}
}

// PreservePending retains session-local retry work during same-worker replacement.
func (w *WMail) PreservePending(old *WMail) {
	if old == nil || old.UserID != w.UserID || old.EmailType != w.EmailType {
		return
	}
	w.unmapPending = old.unmapPending
	w.diagnosticRetries = old.diagnosticRetries
	if old.tracker != nil && w.tracker != nil {
		w.tracker.dirty = old.tracker.dirty
		w.tracker.lastSent = old.tracker.lastSent
	}
}

func (w *WMail) ExecutionContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	if w.Ctx == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(w.Ctx, cancel)
	return ctx, func() { stop(); cancel() }
}
