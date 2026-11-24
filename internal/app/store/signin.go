package store

// Purchased Google mailboxes that connect by sign-in (GraphKit connection method oauth). Google
// Workspace no longer takes new domain-wide delegation grants for this instance, so the mail
// vendor's team signs each mailbox in to this instance's OAuth client from GraphKit:
//
//  1. GraphKit reports the mailbox oauth, not ready. The store mints a single-mailbox client
//     connect link in the purchaser's name (connectlink.CreateSingle), remembers which mailbox it is
//     for, and registers it with GraphKit, which shows it only to the vendor's team.
//  2. The vendor opens it, signs in to Google as the mailbox (the login hint is its address) and
//     allows the app. connectlink connects the mailbox and calls SignInLinks.Connected.
//  3. The store links the mailbox to the purchase, starts warmup, and reports it signed in to
//     GraphKit, which then completes the order.
//
// GraphKit's connection.sign_in decides when a link is minted (none registered, or one about to
// expire), so a registration that failed is repaired by the next webhook or sweep.

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// signInLinkTTL matches how long GraphKit waits for the sign-in before the order item fails.
	signInLinkTTL = 7 * 24 * time.Hour
	// signInRenewBefore replaces a registered link this close to expiring.
	signInRenewBefore = 24 * time.Hour
)

// ConnectLinks mints the single-mailbox client connect links a sign-in rides on; connectlink.Service satisfies it.
type ConnectLinks interface {
	CreateSingle(ctx context.Context, orgID, creator uuid.UUID, label string, provider models.InboxProvider, ttl time.Duration) (*models.CreatedClientConnectLink, *errx.Error)
}

// WireSignIns attaches the connect links and the table of links the store minted; without both, oauth mailboxes wait.
func (s *Service) WireSignIns(links ConnectLinks, repo repository.StoreSignInLinkRepository) {
	if s != nil {
		s.links, s.signIns = links, repo
	}
}

// waitsForSignIn: GraphKit offers the sign-in while the vendor's API still reports the order in
// setup (mailbox provisioning), because the vendor's team finishes the Workspace before its API says so.
func waitsForSignIn(m *graphkit.Mailbox) bool {
	return m.Provider == graphkit.ProviderGoogle && m.Connection.Method == graphkit.ConnectOAuth && !m.Connection.Ready &&
		(m.Status == graphkit.MailboxActive || m.Status == graphkit.MailboxProvisioning)
}

// syncSignIn keeps an oauth mailbox moving: a live link GraphKit can show the vendor, and the
// report once the mailbox is connected here.
func (s *Service) syncSignIn(ctx context.Context, row *models.StoreMailbox, m *graphkit.Mailbox) {
	if row == nil || !waitsForSignIn(m) || s.links == nil || s.signIns == nil {
		return
	}
	if row.EmailAccountID != nil {
		s.reportSignedIn(ctx, row)
		return
	}
	if si := m.Connection.SignIn; si != nil && si.LinkExpiresAt != nil && si.LinkExpiresAt.After(s.now().Add(signInRenewBefore)) {
		return
	}
	creator := s.purchaser(ctx, row)
	if creator == uuid.Nil {
		log.Warn().Str("store_mailbox_id", row.ID.String()).Msg("store: no member to mint a sign-in link in the name of")
		return
	}
	created, xerr := s.links.CreateSingle(ctx, row.OrganizationID, creator, "Mailbox sign-in: "+row.Email, models.InboxProviderGoogle, signInLinkTTL)
	if xerr != nil {
		log.Warn().Str("store_mailbox_id", row.ID.String()).Str("reason", xerr.ResponseCode()).Msg("store: minting a sign-in link failed")
		return
	}
	link := created.Link
	if err := s.signIns.Insert(ctx, repository.StoreSignInLink{LinkID: link.ID, OrganizationID: row.OrganizationID, StoreMailboxID: row.ID,
		Email: strings.ToLower(row.Email), ExpiresAt: link.ExpiresAt}); err != nil {
		log.Error().Err(err).Str("store_mailbox_id", row.ID.String()).Msg("store: recording a sign-in link failed")
		return
	}
	if _, err := s.api().PutMailboxSignInLink(ctx, row.GraphKitMailboxID, created.URL, link.ExpiresAt, "store:signin-link:"+link.ID.String()); err != nil {
		logUpstream("mailbox.sign_in_link", err)
	}
}

// reportSignedIn tells GraphKit the mailbox is connected; a failure is retried by the next webhook or sweep.
func (s *Service) reportSignedIn(ctx context.Context, row *models.StoreMailbox) {
	var err error
	for attempt := range 3 {
		if _, err = s.api().MailboxSignedIn(ctx, row.GraphKitMailboxID, "store:signed-in:"+row.ID.String()); err == nil {
			return
		}
		time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
	}
	logUpstream("mailbox.signed_in", err)
}

// SignInLinks is the store's side of the client connect links it minted (connectlink.PartnerLinks).
func (s *Service) SignInLinks() SignInLinks { return SignInLinks{s: s} }

// SignInLinks implements connectlink.PartnerLinks for the store's sign-in links; it ignores every other link.
type SignInLinks struct{ s *Service }

func (l SignInLinks) get(ctx context.Context, linkID uuid.UUID) *repository.StoreSignInLink {
	if l.s == nil || l.s.signIns == nil {
		return nil
	}
	row, err := l.s.signIns.Get(ctx, linkID)
	if err != nil {
		log.Warn().Err(err).Str("link_id", linkID.String()).Msg("store: reading a sign-in link failed")
		return nil
	}
	return row
}

// LoginHint opens Google's sign-in with the purchased mailbox's address.
func (l SignInLinks) LoginHint(ctx context.Context, linkID uuid.UUID) string {
	if row := l.get(ctx, linkID); row != nil {
		return row.Email
	}
	return ""
}

// ReturnURL: a store link ends on the hosted connect page's own result.
func (SignInLinks) ReturnURL(context.Context, uuid.UUID, string) (string, bool) { return "", false }

// Connected links the mailbox a store link connected to its purchase and reports it to GraphKit.
func (l SignInLinks) Connected(ctx context.Context, linkID uuid.UUID, acc *models.Email) {
	link := l.get(ctx, linkID)
	if link == nil || acc == nil {
		return
	}
	s := l.s
	row, err := s.Repo.GetMailbox(ctx, link.OrganizationID, link.StoreMailboxID)
	if err != nil || row == nil {
		log.Warn().Str("link_id", linkID.String()).Msg("store: the mailbox of a sign-in link is gone")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(acc.Email), row.Email) {
		// Someone signed in as another account: that mailbox is in the workspace now, but it is not this purchase.
		log.Warn().Str("store_mailbox_id", row.ID.String()).Msg("store: a sign-in link connected a different mailbox than the one it was for")
		return
	}
	if row.EmailAccountID == nil {
		s.connected(ctx, row, acc)
		id := acc.ID
		row.EmailAccountID = &id
	}
	s.reportSignedIn(ctx, row)
}

// PartnerLinksChain asks each in turn: the store's links first, then the partner API's.
type PartnerLinksChain []interface {
	LoginHint(ctx context.Context, linkID uuid.UUID) string
	Connected(ctx context.Context, linkID uuid.UUID, acc *models.Email)
	ReturnURL(ctx context.Context, linkID uuid.UUID, status string) (string, bool)
}

// LoginHint is the first non-empty hint.
func (c PartnerLinksChain) LoginHint(ctx context.Context, linkID uuid.UUID) string {
	for _, p := range c {
		if h := p.LoginHint(ctx, linkID); h != "" {
			return h
		}
	}
	return ""
}

// Connected tells each one; each ignores links it did not mint.
func (c PartnerLinksChain) Connected(ctx context.Context, linkID uuid.UUID, acc *models.Email) {
	for _, p := range c {
		p.Connected(ctx, linkID, acc)
	}
}

// ReturnURL is the first one that knows the link.
func (c PartnerLinksChain) ReturnURL(ctx context.Context, linkID uuid.UUID, status string) (string, bool) {
	for _, p := range c {
		if u, ok := p.ReturnURL(ctx, linkID, status); ok {
			return u, true
		}
	}
	return "", false
}
