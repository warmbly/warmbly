package wmail

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// onGraphMessageRemove emits REMOVE_EMAIL for a message deleted or moved out of a
// tracked folder. providerID is the Graph message id.
func (w *WMail) onGraphMessageRemove(ctx context.Context, providerID string) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, providerID)
	if err != nil {
		return err
	}
	if internalMessage == nil {
		return nil
	}

	internalID, err := uuid.Parse(internalMessage.ID)
	if err != nil {
		return err
	}

	return w.onEvent(models.JobEventTypeRemoveEmail, &models.JobEventRemoveEmail{
		UserID:  w.UserID,
		EmailID: w.ID,
		ID:      internalID,
	})
}

// onGraphFlagsChange keeps read state in sync: Graph delta reports isRead, which
// we map to the \Seen flag add/remove the unibox already understands. No-op when
// the message isn't tracked yet (the add path sets the initial flags).
func (w *WMail) onGraphFlagsChange(ctx context.Context, providerID string, seen bool) error {
	internalMessage, err := w.EmailMessageMapRepository.Get(ctx, w.UserID, w.ID, providerID)
	if err != nil {
		return err
	}
	if internalMessage == nil {
		return nil
	}

	internalID, err := uuid.Parse(internalMessage.ID)
	if err != nil {
		return err
	}

	eventType := models.JobEventTypeFlagsRemove
	if seen {
		eventType = models.JobEventTypeFlagsAdd
	}

	return w.onEvent(eventType, &models.JobEventFlags{
		UserID:  w.UserID,
		EmailID: w.ID,
		ID:      internalID,
		Flags:   []string{"\\Seen"},
	})
}

// onGraphDelta relays the opaque per-folder delta cursor to the control plane for
// durable persistence (the worker is disposable and must not be the source of
// truth for the cursor).
func (w *WMail) onGraphDelta(_ context.Context, folder, deltaLink string) error {
	return w.onEvent(models.JobEventTypeGraphDeltaUpdate, &models.JobEventGraphDeltaUpdate{
		UserID:    w.UserID,
		EmailID:   w.ID,
		Folder:    folder,
		DeltaLink: deltaLink,
	})
}

// cloneStringMap returns a shallow copy so the worker's live cursor map is never
// aliased to the deserialized event payload.
func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
