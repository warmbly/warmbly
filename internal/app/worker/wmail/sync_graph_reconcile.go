package wmail

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/msgraph"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const graphIdentityCursor = "graph:immutable-ids"

type graphDeltaRecovery struct {
	Version     int       `json:"version"`
	Since       time.Time `json:"since"`
	Count       int       `json:"count"`
	Baseline    string    `json:"baseline,omitempty"`
	PendingLink string    `json:"pending_link,omitempty"`
}

func (w *WMail) graphRecovery(folder string) (graphDeltaRecovery, error) {
	var recovery graphDeltaRecovery
	data := w.tracker.folder("graph:delta-recovery:" + folder).Next
	if data == "" {
		return recovery, nil
	}
	if err := json.Unmarshal([]byte(data), &recovery); err != nil {
		return recovery, err
	}
	if recovery.Version != 1 || recovery.Since.IsZero() || recovery.Count < 0 {
		return recovery, errors.New("graph: unsupported recovery state")
	}
	return recovery, nil
}

func (w *WMail) setGraphRecovery(folder string, recovery graphDeltaRecovery) {
	data, _ := json.Marshal(recovery)
	w.tracker.setFolder("graph:delta-recovery:"+folder, models.SyncFolderCursor{Next: string(data)})
}

func (w *WMail) graphRecovering(folder string) bool {
	recovery, err := w.graphRecovery(folder)
	if err != nil {
		return true
	}
	key := "graph:delta-recovery:" + folder
	cur := w.tracker.folder(key)
	if recovery.Version == 0 || cur.Done {
		return false
	}
	if recovery.Baseline != "" && recovery.Baseline == w.GraphData.Client.DeltaLinks[folder] {
		cur.Done = true
		w.tracker.setFolder(key, cur)
		return false
	}
	return true
}

func (w *WMail) graphPendingRecoveryLink(folder string) (string, error) {
	recovery, err := w.graphRecovery(folder)
	if err != nil {
		return "", err
	}
	if w.graphRecovering(folder) {
		return recovery.PendingLink, nil
	}
	return "", nil
}

func (w *WMail) graphRecoveryBaseline(ctx context.Context, folder, link string) error {
	recovery, err := w.graphRecovery(folder)
	if err != nil {
		return err
	}
	recovery.PendingLink = link
	w.setGraphRecovery(folder, recovery)
	return w.tracker.emit(w.tracker.state)
}

func (w *WMail) graphRecoveryStart(ctx context.Context, folder string) error {
	recovery, err := w.graphRecovery(folder)
	if err != nil {
		return err
	}
	if recovery.Version == 0 || !w.graphRecovering(folder) {
		since := time.Now().Add(-time.Duration(w.gov.Policy().BackfillDays) * 24 * time.Hour)
		if w.tracker.state.BackfillStatus != models.SyncBackfillComplete && w.tracker.state.BackfillSince != nil {
			since = *w.tracker.state.BackfillSince
		}
		recovery = graphDeltaRecovery{Version: 1, Since: since}
	}
	recovery.Baseline = ""
	w.setGraphRecovery(folder, recovery)
	w.tracker.clearFolder("graph:reconcile:" + folder)
	return w.tracker.emit(w.tracker.state)
}

func (w *WMail) graphRecoveryCheckpoint(ctx context.Context, folder, link string) error {
	recovery, err := w.graphRecovery(folder)
	if err != nil {
		return err
	}
	recovery.Baseline = link
	w.setGraphRecovery(folder, recovery)
	return w.tracker.emit(w.tracker.state)
}

func (w *WMail) graphMessageMap(ctx context.Context, providerID string) (*repository.EmailMessageData, error) {
	known, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, providerID)
	if err != nil || known != nil || !w.GraphData.Client.ImmutableIDMode() {
		return known, err
	}
	ids, err := w.GraphData.Client.RegularMessageIDs(ctx, []string{providerID})
	if err != nil {
		return nil, err
	}
	known, err = w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, ids[providerID])
	if err != nil || known == nil {
		return known, err
	}
	alias := *known
	alias.MessageID = providerID
	if err := w.graphAddAlias(ctx, alias); err != nil {
		return nil, err
	}
	return &alias, nil
}

func (w *WMail) graphAddAlias(ctx context.Context, alias repository.EmailMessageData) error {
	existing, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, alias.MessageID)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.ID != alias.ID {
			return errors.New("graph: conflicting immutable message mapping")
		}
		return nil
	}
	if err := w.EmailMessageMapRepository.Add(ctx, alias); err != nil {
		return err
	}
	stored, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, alias.MessageID)
	if err != nil {
		return err
	}
	if stored == nil || stored.ID != alias.ID {
		return errors.New("graph: immutable message mapping not retained")
	}
	return nil
}

func (w *WMail) graphSyncError(err error) *errx.MailError {
	var merr *errx.MailError
	if errors.As(err, &merr) {
		return merr
	}
	w.CaptureError(err)
	return nil
}

func (w *WMail) graphStoredPage(ctx context.Context, key string) ([]repository.ProviderFolderMessage, error) {
	var after *uuid.UUID
	if next := w.tracker.folder(key).Next; next != "" {
		id, err := uuid.Parse(next)
		if err != nil {
			return nil, err
		}
		after = &id
	}
	return w.SyncContext.ListProviderMessages(ctx, w.UserID, w.ID, after, graphBackfillPage)
}

func (w *WMail) graphUpgradeIDs(ctx context.Context) (bool, error) {
	if w.tracker.folder(graphIdentityCursor).Done {
		return true, nil
	}
	rows, err := w.graphStoredPage(ctx, graphIdentityCursor)
	if err != nil {
		return false, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ProviderID)
	}
	var converted map[string]string
	if len(ids) > 0 {
		converted, err = w.GraphData.Client.ImmutableMessageIDs(ctx, ids)
		if err != nil {
			return false, err
		}
	}
	cur := w.tracker.folder(graphIdentityCursor)
	for _, row := range rows {
		id := converted[row.ProviderID]
		known, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, row.ProviderID)
		if err != nil {
			return false, err
		}
		if known == nil {
			return false, errors.New("graph: saved message mapping missing during id upgrade")
		}
		alias := *known
		alias.MessageID = id
		if err := w.graphAddAlias(ctx, alias); err != nil {
			return false, err
		}
		// Relayed placement updates only the provider id, preserving customer filing.
		if err := w.onEvent(models.JobEventTypeFolderUpdate, &models.JobEventFolderUpdate{
			UserID: w.UserID, EmailID: w.ID, ID: row.ID, ProviderID: id,
			Folder: row.ProviderFolder, Relayed: true,
		}); err != nil {
			return false, err
		}
		cur.Next = row.ID.String()
		w.tracker.setFolder(graphIdentityCursor, cur)
	}
	cur.Done = len(rows) < graphBackfillPage
	w.tracker.setFolder(graphIdentityCursor, cur)
	w.tracker.flush(time.Now())
	return cur.Done, nil
}

func (w *WMail) graphReconcileMessage(ctx context.Context, internalID, providerID, fallback string) error {
	id, err := uuid.Parse(internalID)
	if err != nil {
		return err
	}
	full, err := w.GraphData.Client.FetchMessage(ctx, fallback, providerID)
	if err != nil {
		return err
	}
	if full == nil {
		return w.onEvent(models.JobEventTypeRemoveEmail, &models.JobEventRemoveEmail{UserID: w.UserID, EmailID: w.ID, ID: id})
	}
	folder, err := w.GraphData.Client.MessageFolder(ctx, full, fallback)
	if err != nil {
		return err
	}
	if folder != "" {
		if err := w.emitFolder(id, folder); err != nil {
			return err
		}
	}
	return w.onGraphFlagsChange(ctx, providerID, full.IsRead)
}

func (w *WMail) graphReconcileFolder(ctx context.Context, folder string) (bool, error) {
	key := "graph:reconcile:" + folder
	rows, err := w.graphStoredPage(ctx, key)
	if err != nil {
		return false, err
	}
	canonical := (&msgraph.GraphMessage{}).ToEmailData(folder).Folder
	cur := w.tracker.folder(key)
	for _, row := range rows {
		if row.ProviderFolder == canonical {
			if err := w.graphReconcileMessage(ctx, row.ID.String(), row.ProviderID, folder); err != nil {
				return false, err
			}
		}
		cur.Next = row.ID.String()
		w.tracker.setFolder(key, cur)
	}
	done := len(rows) < graphBackfillPage
	if done {
		w.tracker.clearFolder(key)
	}
	return done, nil
}
