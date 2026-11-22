package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// A signature Google or Warmbly did not take is retried at the reconciler's pace, for about a day.
const (
	signatureRetryAfter  = 10 * time.Minute
	signatureMaxAttempts = 144
)

// MailboxSignature is an order line's finished signature, filled in by the checkout; both empty is no signature.
type MailboxSignature struct {
	HTML  string `json:"html"`
	Plain string `json:"plain"`
}

// cleanSignatures bounds each line's signature like a mailbox's own, refusing one for an address the order does not buy.
func cleanSignatures(in map[string]MailboxSignature, items []graphkit.QuoteItem) (map[string]MailboxSignature, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := map[string]MailboxSignature{}
	for _, it := range items {
		if it.Type != graphkit.ItemMailboxes {
			continue
		}
		for _, m := range it.Mailboxes {
			key := photoKey(m.LocalPart + "@" + it.Domain)
			sig, ok := in[key]
			if !ok {
				continue
			}
			if utf8.RuneCountInString(sig.HTML) > config.SignatureHTMLMax || utf8.RuneCountInString(sig.Plain) > config.SignaturePlainMax {
				return nil, itemsErr(fmt.Sprintf("A signature is longer than a mailbox keeps (%d characters of HTML, %d of plain text).", config.SignatureHTMLMax, config.SignaturePlainMax))
			}
			if strings.TrimSpace(sig.HTML) == "" && strings.TrimSpace(sig.Plain) == "" {
				sig = MailboxSignature{}
			}
			out[key] = sig
		}
	}
	if len(out) != len(in) {
		return nil, itemsErr("A signature goes on a mailbox in the order.")
	}
	return out, nil
}

// adoptOrderSignature gives a mailbox the signature its order line asked for, unless it already has one.
func (s *Service) adoptOrderSignature(ctx context.Context, row *models.StoreMailbox, snap quoteSnapshot) {
	if row.SignatureHTML != nil || len(snap.Signatures) == 0 {
		return
	}
	sig, ok := snap.Signatures[photoKey(row.Email)]
	if !ok {
		return
	}
	if set, err := s.Repo.SetMailboxSignature(ctx, row.OrganizationID, row.ID, sig.HTML, sig.Plain); err != nil || !set {
		return
	}
	row.SignatureHTML, row.SignaturePlain = &sig.HTML, sig.Plain
	if row.EmailAccountID == nil {
		// The connect applies it.
		return
	}
	m := *row
	s.connects.Add(1)
	go func() {
		defer s.connects.Done()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		s.applySignature(sctx, &m)
	}()
}

// applySignature sets a connected mailbox's chosen signature in Warmbly, then at its provider where one has an API.
// Each half is stamped once done, so a retry never overwrites an edit made since. True once both are done.
func (s *Service) applySignature(ctx context.Context, m *models.StoreMailbox) bool {
	if m.SignatureHTML == nil || m.EmailAccountID == nil || !liveStatus(m.Status) {
		return false
	}
	if m.SignatureSetAt != nil && m.SignatureProviderAt != nil {
		return true
	}
	// The connect, the order's adoption and the reconciler can all reach one mailbox at once; only the claimant writes.
	m, err := s.Repo.ClaimSignature(ctx, m.OrganizationID, m.ID, s.now().Add(-signatureRetryAfter), signatureMaxAttempts)
	if err != nil || m == nil {
		return false
	}
	local, provider := m.SignatureSetAt != nil, m.SignatureProviderAt != nil
	giveUp := false
	if !local && s.Mailboxes != nil {
		h, plain := *m.SignatureHTML, m.SignaturePlain
		sync := h != "" || plain != ""
		upd := &models.UpdateEmail{SignatureHTML: &h, SignaturePlain: &plain, SignatureSync: &sync}
		if _, xerr := s.Mailboxes.Update(ctx, m.OrganizationID.String(), s.purchaser(ctx, m).String(), m.EmailAccountID.String(), upd); xerr != nil {
			log.Warn().Str("store_mailbox_id", m.ID.String()).Str("reason", xerr.ResponseCode()).Msg("store: setting a purchased mailbox's signature failed")
		} else {
			local = true
		}
	}
	if !provider {
		var err error
		provider, err = s.providerSignature(ctx, m)
		if err != nil {
			log.Warn().Err(err).Str("store_mailbox_id", m.ID.String()).Msg("store: setting a purchased mailbox's provider signature failed")
			giveUp = errors.Is(err, delegation.ErrSignatureRefused)
		}
	}
	if local || provider {
		if err := s.Repo.MarkSignatureApplied(context.WithoutCancel(ctx), m.OrganizationID, m.ID, local, provider); err != nil {
			log.Warn().Err(err).Str("store_mailbox_id", m.ID.String()).Msg("store: recording the mailbox signature failed")
			return false
		}
	}
	if local && provider {
		return true
	}
	_ = s.Repo.RecordSignatureFailure(context.WithoutCancel(ctx), m.OrganizationID, m.ID, giveUp)
	return false
}

// providerSignature sets the Gmail send-as signature of a Google mailbox Warmbly reaches by token. Microsoft
// has no signature API and a mailbox connected by password has no token, so those sign in Warmbly only.
func (s *Service) providerSignature(ctx context.Context, m *models.StoreMailbox) (bool, error) {
	if *m.SignatureHTML == "" || m.Provider != graphkit.ProviderGoogle || s.Delegator == nil {
		return true, nil
	}
	switch m.ConnectMethod {
	case graphkit.ConnectToken, graphkit.ConnectDelegation:
	default:
		return true, nil
	}
	if err := s.Delegator.SetPurchasedSignature(ctx, m.OrganizationID, m.Email, *m.SignatureHTML, m.ConnectMethod == graphkit.ConnectToken); err != nil {
		return false, err
	}
	return true, nil
}

// retrySignatures sets the signatures not taken yet.
func (s *Service) retrySignatures(ctx context.Context) error {
	rows, err := s.Repo.PendingSignatures(ctx, s.now().Add(-signatureRetryAfter), signatureMaxAttempts, 50)
	if err != nil {
		return err
	}
	for i := range rows {
		sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		s.applySignature(sctx, &rows[i])
		cancel()
	}
	return nil
}
