package slackapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
)

// linkCodeLen is the base64url length of a 32-byte code.
const linkCodeLen = 43

func hashLinkCode(code string) []byte {
	sum := sha256.Sum256([]byte(code))
	return sum[:]
}

// mintLinkURL stores a fresh single-use code for a Slack member and returns
// the dashboard URL that redeems it ("" when the instance has no APP_URL).
func (s *Service) mintLinkURL(ctx context.Context, conn *models.IntegrationConnection, teamID, slackUserID string) string {
	u, _ := s.mintLink(ctx, conn, teamID, slackUserID)
	return u
}

// mintLink is mintLinkURL that also returns the code, to hold a question under.
func (s *Service) mintLink(ctx context.Context, conn *models.IntegrationConnection, teamID, slackUserID string) (string, string) {
	if appURL("/") == "" {
		return "", ""
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", ""
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	err := s.repo.CreateLinkCode(ctx, hashLinkCode(code), models.SlackLinkCode{
		OrganizationID: conn.OrganizationID,
		ConnectionID:   conn.ID,
		SlackTeamID:    teamID,
		SlackUserID:    slackUserID,
		ExpiresAt:      time.Now().Add(linkCodeTTL),
	})
	if err != nil {
		log.Warn().Err(err).Msg("slack: storing a link code failed")
		return "", ""
	}
	return appURL("/slack/link?code=" + url.QueryEscape(code)), code
}

// LinkPreview is GET /v1/integrations/slack/link/:code.
type LinkPreview struct {
	OrganizationID   uuid.UUID `json:"organization_id"`
	OrganizationName string    `json:"organization_name"`
	IsMember         bool      `json:"is_member"`
	// UserEmail is the signed-in Warmbly account the link would be made for.
	UserEmail string `json:"user_email"`
	// VerifyAvailable: Sign in with Slack can confirm the link when the
	// emails do not match.
	VerifyAvailable bool `json:"verify_available"`
	models.SlackLinkPreview
}

func validCode(code string) bool {
	if len(code) != linkCodeLen {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(code)
	return err == nil
}

// PreviewLink describes a pending code to the signed-in user before they confirm.
func (s *Service) PreviewLink(ctx context.Context, userID uuid.UUID, code string) (*LinkPreview, *errx.Error) {
	code = strings.TrimSpace(code)
	if !validCode(code) {
		return nil, ErrSlackLinkInvalid
	}
	c, err := s.repo.PreviewLinkCode(ctx, hashLinkCode(code))
	if err != nil {
		return nil, errx.InternalError()
	}
	if c == nil {
		return nil, ErrSlackLinkInvalid
	}
	prof, xerr := s.slackProfile(ctx, c)
	if xerr != nil {
		return nil, xerr
	}
	out := &LinkPreview{
		OrganizationID: c.OrganizationID,
		SlackLinkPreview: models.SlackLinkPreview{
			SlackTeamID:     c.SlackTeamID,
			SlackUserID:     c.SlackUserID,
			SlackUserName:   prof.Name,
			SlackUserAvatar: prof.Avatar,
			ExpiresAt:       c.ExpiresAt,
		},
	}
	if s.users != nil {
		if u, err := s.users.GetUser(ctx, userID); err == nil && u != nil {
			out.UserEmail = u.Email
			out.EmailMatches = models.SlackLinkEmailMatches(prof.Email, u.Email)
		}
	}
	out.VerifyAvailable = s.VerifyAvailable()
	if org, xerr := s.orgs.Get(ctx, c.OrganizationID); xerr == nil && org != nil {
		out.OrganizationName = org.Name
	}
	if m, xerr := s.orgs.GetMembership(ctx, c.OrganizationID, userID); xerr == nil && m != nil && m.AcceptedAt != nil {
		out.IsMember = true
	}
	if conns, err := s.integ.ListConnections(ctx, c.OrganizationID); err == nil {
		for _, conn := range conns {
			if conn.ID == c.ConnectionID {
				out.SlackTeamName = conn.ExternalAccountName
			}
		}
	}
	return out, nil
}

// linkProfile is the Slack account a code names, as Slack reports it now.
type linkProfile struct {
	Name   string
	Email  string
	Avatar string
}

// profileFrom keeps a name safe to show, an https avatar, and the email only
// when Slack has not marked it unconfirmed.
func profileFrom(u *slackUser) linkProfile {
	var p linkProfile
	if u == nil || u.Deleted || u.IsBot {
		return p
	}
	for _, n := range []string{u.Profile.DisplayName, u.Profile.RealName} {
		if p.Name = displayname.Clean(n, displayname.Person); p.Name != "" {
			break
		}
	}
	if u.IsEmailConfirmed == nil || *u.IsEmailConfirmed {
		p.Email = strings.TrimSpace(u.Profile.Email)
	}
	if v, err := url.Parse(u.Profile.Image72); err == nil && v.Scheme == "https" && v.Host != "" {
		p.Avatar = v.String()
	}
	return p
}

// slackProfile reads the code's Slack account. An install without the users
// scopes yields an empty profile, which no Warmbly email matches.
func (s *Service) slackProfile(ctx context.Context, c *models.SlackLinkCode) (linkProfile, *errx.Error) {
	token, err := s.integ.SlackBotToken(ctx, c.OrganizationID, c.ConnectionID)
	if err != nil {
		return linkProfile{}, ErrSlackLinkInvalid
	}
	u, err := s.client.UserInfo(ctx, token, c.SlackUserID)
	switch {
	case IsAPIError(err, "missing_scope", "user_not_found"):
		return linkProfile{}, nil
	case err != nil:
		return linkProfile{}, errx.NewPublic(errx.ServiceUnavailable, "Slack could not be reached to check this account. Try again in a moment.")
	}
	return profileFrom(u), nil
}

// ConfirmLink redeems a code for the signed-in user, who must be an accepted
// member of the code's workspace, and either carries a Sign in with Slack
// proof for the code's Slack account or has that account's email. The code
// is spent in the same transaction.
func (s *Service) ConfirmLink(ctx context.Context, userID uuid.UUID, code string, proof *LinkProof) (*models.SlackUserLink, *errx.Error) {
	code = strings.TrimSpace(code)
	if !validCode(code) {
		return nil, ErrSlackLinkInvalid
	}
	c, err := s.repo.PreviewLinkCode(ctx, hashLinkCode(code))
	if err != nil {
		return nil, errx.InternalError()
	}
	if c == nil {
		return nil, ErrSlackLinkInvalid
	}
	verified := proof != nil && proof.Code != ""
	email := ""
	if verified {
		if xerr := s.verifyProof(ctx, userID, code, c, *proof); xerr != nil {
			return nil, xerr
		}
	} else {
		prof, xerr := s.slackProfile(ctx, c)
		if xerr != nil {
			return nil, xerr
		}
		email = prof.Email
	}
	link, err := s.repo.ConsumeLinkCode(ctx, hashLinkCode(code), userID, email, verified)
	switch {
	case errors.Is(err, repository.ErrSlackLinkCodeInvalid):
		return nil, ErrSlackLinkInvalid
	case errors.Is(err, repository.ErrSlackLinkNotMember):
		return nil, errSlackLinkNotMember
	case errors.Is(err, repository.ErrSlackLinkEmailMismatch):
		return nil, ErrSlackLinkEmailMismatch
	case err != nil || link == nil:
		return nil, errx.InternalError()
	}
	l := *link
	s.spawn("link_confirmation", agentRunTimeout, func(ctx context.Context) {
		q := s.takeAsk(ctx, l.OrganizationID, code)
		s.sendLinkConfirmation(ctx, &l, q != nil)
		if q != nil {
			s.resumeAsk(ctx, &l, q)
		}
	})
	return link, nil
}

// SlackInstalled implements integration.SlackInstallHook: the member who
// connected Slack is linked to the Slack account that approved the install
// when the two share an email.
func (s *Service) SlackInstalled(_ context.Context, conn *models.IntegrationConnection, slackUserID string, userID uuid.UUID) {
	if conn == nil || slackUserID == "" || conn.ExternalAccountID == "" {
		return
	}
	// Best effort, so it never holds up the OAuth callback on a Slack call.
	c := *conn
	s.spawn("installer_link", shortTaskTime, func(ctx context.Context) {
		s.linkInstaller(ctx, &c, slackUserID, userID)
	})
}

func (s *Service) linkInstaller(ctx context.Context, conn *models.IntegrationConnection, slackUserID string, userID uuid.UUID) {
	token, err := s.integ.SlackBotToken(ctx, conn.OrganizationID, conn.ID)
	if err != nil {
		return
	}
	// The same rule as a link button: only the Warmbly account with the Slack
	// account's email is linked; anyone else links by asking the bot.
	u, err := s.client.UserInfo(ctx, token, slackUserID)
	if err != nil {
		return
	}
	link, err := s.repo.LinkInstaller(ctx, conn.OrganizationID, conn.ID, conn.ExternalAccountID, slackUserID, profileFrom(u).Email, userID)
	if err != nil {
		log.Warn().Err(err).Msg("slack: linking the installer failed")
		return
	}
	if link == nil {
		return
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, link.OrganizationID, userID, models.AuditActionCreate, models.AuditEntityIntegration,
			&link.ConnectionID, "", "Slack", nil, map[string]string{"slack_link": "linked_on_install"})
	}
	s.sendLinkConfirmation(ctx, link, false)
}

// autoLinkMissTTL is how long a Slack member with no matching Warmbly
// account is left alone before their email is looked up again.
const autoLinkMissTTL = 10 * time.Minute

// autoLink links an unlinked Slack member to the Warmbly account with their
// Slack email in any workspace connected to the team, so a member whose
// emails match never sees a link page. A miss is remembered for a while.
func (s *Service) autoLink(ctx context.Context, a *actor) bool {
	if a == nil || a.link != nil || a.unknown || a.restricted || s.users == nil {
		return false
	}
	missKey := "slack:autolink:miss:" + a.teamID + ":" + a.userID
	if s.guard.get(ctx, missKey) != "" {
		return false
	}
	miss := func() bool {
		s.guard.put(ctx, missKey, "1", autoLinkMissTTL)
		return false
	}
	u, err := s.client.UserInfo(ctx, a.token, a.userID)
	if err != nil {
		if IsAPIError(err, "missing_scope", "user_not_found") {
			return miss()
		}
		return false
	}
	email := profileFrom(u).Email
	if !strings.Contains(email, "@") {
		return miss()
	}
	wu, err := s.users.GetUserByEmail(ctx, email)
	if err != nil || wu == nil {
		return miss()
	}
	conns, err := s.integ.SlackConnectionsForTeam(ctx, a.teamID)
	if err != nil {
		return false
	}
	for i := range conns {
		conn := &conns[i]
		link, err := s.repo.LinkInstaller(ctx, conn.OrganizationID, conn.ID, a.teamID, a.userID, email, wu.ID)
		if err != nil {
			log.Warn().Err(err).Msg("slack: linking by email failed")
			return false
		}
		if link == nil {
			continue
		}
		token := a.token
		if conn.ID != a.conn.ID {
			if token, err = s.integ.SlackBotToken(ctx, conn.OrganizationID, conn.ID); err != nil {
				return false
			}
		}
		m, st := s.membership(ctx, link)
		if st != memberOK {
			return false
		}
		a.conn, a.token, a.link, a.member, a.gone = conn, token, link, m, false
		if s.audit != nil {
			s.audit.LogAction(ctx, link.OrganizationID, link.UserID, models.AuditActionCreate, models.AuditEntityIntegration,
				&link.ConnectionID, "", "Slack", nil, map[string]string{"slack_link": "linked_by_email"})
		}
		s.sendAutoLinkNotice(ctx, a)
		return true
	}
	return miss()
}

func (s *Service) sendAutoLinkNotice(ctx context.Context, a *actor) {
	dm, err := s.client.OpenDM(ctx, a.token, a.userID)
	if err != nil {
		return
	}
	text := s.linkedLine(ctx, a) + " Your Slack email matches your Warmbly account, so I linked them for you. Unlink any time from my Home tab."
	if _, err := s.client.PostMessage(ctx, a.token, Message{Channel: dm, Text: "You're linked to Warmbly", Blocks: blocks(sectionBlock(text))}); err != nil {
		log.Warn().Err(err).Msg("slack: auto-link notice DM failed")
	}
}

// resumeAsk answers the question that was held while its author linked.
func (s *Service) resumeAsk(ctx context.Context, link *models.SlackUserLink, q *ask) {
	a := s.resolveActor(ctx, link.SlackTeamID, link.SlackUserID)
	if a == nil || a.link == nil {
		return
	}
	s.handleAsk(ctx, a, *q)
}

func (s *Service) sendLinkConfirmation(ctx context.Context, link *models.SlackUserLink, resuming bool) {
	token, err := s.integ.SlackBotToken(ctx, link.OrganizationID, link.ConnectionID)
	if err != nil {
		return
	}
	dm, err := s.client.OpenDM(ctx, token, link.SlackUserID)
	if err != nil {
		return
	}
	orgName := "your Warmbly workspace"
	if org, xerr := s.orgs.Get(ctx, link.OrganizationID); xerr == nil && org != nil && org.Name != "" {
		orgName = "*" + escapeMrkdwn(org.Name) + "*"
	}
	text := "You're linked to " + orgName + ". Ask me anything about your outreach right here, or mention @Warmbly in any channel I'm in."
	if resuming {
		text = "You're linked to " + orgName + ". I'm answering your question now."
	}
	if _, err := s.client.PostMessage(ctx, token, Message{Channel: dm, Text: "You're linked to Warmbly", Blocks: blocks(sectionBlock(text))}); err != nil {
		log.Warn().Err(err).Msg("slack: link confirmation DM failed")
	}
}

// UpdateMyLink toggles the caller's DM notifications.
func (s *Service) UpdateMyLink(ctx context.Context, orgID, userID uuid.UUID, dm bool) (*models.SlackUserLink, *errx.Error) {
	link, err := s.repo.SetLinkDMNotifications(ctx, orgID, userID, dm)
	if err != nil {
		return nil, errx.InternalError()
	}
	if link == nil {
		return nil, errx.NewWithIdentifier(errx.NotFound, "slack_not_linked", "You have not linked a Slack account in this workspace.")
	}
	return link, nil
}

// UnlinkMine removes the caller's own link; removing nothing is not an error.
func (s *Service) UnlinkMine(ctx context.Context, orgID, userID uuid.UUID) *errx.Error {
	if _, err := s.repo.DeleteLinkForUser(ctx, orgID, userID); err != nil {
		return errx.InternalError()
	}
	return nil
}

// RemoveLink removes any member's link in the org.
func (s *Service) RemoveLink(ctx context.Context, orgID, linkID uuid.UUID) (*models.SlackUserLink, *errx.Error) {
	link, err := s.repo.DeleteLink(ctx, orgID, linkID)
	if err != nil {
		return nil, errx.InternalError()
	}
	if link == nil {
		return nil, errx.New(errx.NotFound, "Slack link not found.")
	}
	return link, nil
}

func (s *Service) linkedLine(ctx context.Context, a *actor) string {
	org := "your Warmbly workspace"
	if o, xerr := s.orgs.Get(ctx, a.link.OrganizationID); xerr == nil && o != nil && o.Name != "" {
		org = "*" + escapeMrkdwn(o.Name) + "*"
	}
	who := ""
	if a.link.UserName != "" {
		who = " as *" + escapeMrkdwn(a.link.UserName) + "*"
	}
	return "You're linked to " + org + who + "."
}

// auditUnlink records a link removed from Slack on the audit spine.
func (s *Service) auditUnlink(ctx context.Context, link *models.SlackUserLink) {
	if s.audit == nil || link == nil {
		return
	}
	s.audit.LogAction(ctx, link.OrganizationID, link.UserID, models.AuditActionDelete, models.AuditEntityIntegration,
		&link.ConnectionID, "", "Slack", nil, map[string]string{"slack_link": "removed_from_slack"})
}
