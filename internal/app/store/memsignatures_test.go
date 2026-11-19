package store

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func (r *memRepo) SetMailboxSignature(_ context.Context, orgID, id uuid.UUID, html, plain string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailboxes[id]
	if m == nil || m.OrganizationID != orgID || m.SignatureHTML != nil {
		return false, nil
	}
	m.SignatureHTML, m.SignaturePlain = &html, plain
	delete(r.signatureTry, id)
	return true, nil
}

func (r *memRepo) MarkSignatureApplied(_ context.Context, orgID, id uuid.UUID, local, provider bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailboxes[id]
	if m == nil || m.OrganizationID != orgID {
		return nil
	}
	now := r.now()
	if local && m.SignatureSetAt == nil {
		m.SignatureSetAt = &now
	}
	if provider && m.SignatureProviderAt == nil {
		m.SignatureProviderAt = &now
	}
	t := r.signatureTry[id]
	t.at = now
	r.signatureTry[id] = t
	return nil
}

func (r *memRepo) RecordSignatureFailure(_ context.Context, orgID, id uuid.UUID, giveUp bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailboxes[id]; m == nil || m.OrganizationID != orgID {
		return nil
	}
	t := r.signatureTry[id]
	t.attempts++
	if giveUp {
		t.attempts = 1000
	}
	t.at = r.now()
	r.signatureTry[id] = t
	return nil
}

func (r *memRepo) ClaimSignature(_ context.Context, orgID, id uuid.UUID, triedBefore time.Time, maxAttempts int) (*models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailbox(orgID, id)
	if m == nil || !r.signaturePending(m, triedBefore, maxAttempts) {
		return nil, nil
	}
	t := r.signatureTry[id]
	t.at = r.now()
	r.signatureTry[id] = t
	return cp(m), nil
}

func (r *memRepo) signaturePending(m *models.StoreMailbox, triedBefore time.Time, maxAttempts int) bool {
	if m.SignatureHTML == nil || m.EmailAccountID == nil || (m.SignatureSetAt != nil && m.SignatureProviderAt != nil) || !liveStatus(m.Status) {
		return false
	}
	t := r.signatureTry[m.ID]
	return t.attempts < maxAttempts && (t.at.IsZero() || t.at.Before(triedBefore))
}

func (r *memRepo) PendingSignatures(_ context.Context, triedBefore time.Time, maxAttempts, limit int) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if !r.signaturePending(m, triedBefore, maxAttempts) {
			continue
		}
		out = append(out, *m)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
