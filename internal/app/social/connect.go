package social

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// ConnectQuote is GET /v1/social/connect-quote: what connecting an account takes now.
type ConnectQuote struct {
	MonthlyMicros int64         `json:"monthly_micros"`
	Tier          string        `json:"tier"`
	DueNowMicros  int64         `json:"due_now_micros"`
	Payment       store.Payment `json:"payment"`
	// Free is a reconnect, a linked GraphKit workspace, or an instance that charges nothing.
	Free bool `json:"free"`
}

// Quote prices a connect: a new managed seat pays its first month now, a reconnect or a linked workspace nothing.
func (s *Service) Quote(ctx context.Context, orgID uuid.UUID, accountID *uuid.UUID) (*ConnectQuote, error) {
	if !s.available() {
		return nil, errUnavailable
	}
	price, err := s.seatPrice(ctx, orgID)
	if err != nil {
		return nil, err
	}
	q := &ConnectQuote{MonthlyMicros: price.MonthlyMicros, Tier: price.Tier, Free: true}
	if accountID != nil {
		if _, err := s.live(ctx, orgID, *accountID); err != nil {
			return nil, err
		}
		return q, nil
	}
	mode, err := s.mode(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if mode == models.SocialModeLinked {
		q.MonthlyMicros = 0
		return q, nil
	}
	if err := s.requireBuyer(ctx, orgID); err != nil {
		return nil, err
	}
	if s.Store.Metered() && price.MonthlyMicros > 0 {
		q.DueNowMicros = price.MonthlyMicros
		q.Free = false
	}
	q.Payment = s.Store.PaymentFor(ctx, orgID, q.DueNowMicros, 0)
	return q, nil
}

// ConnectRequest is POST /v1/social/connections.
type ConnectRequest struct {
	// Channel is the network to connect; a reconnect keeps its account's.
	Channel string `json:"channel"`
	// AccountID reconnects one of the workspace's accounts; nil connects a new one.
	AccountID    *uuid.UUID `json:"account_id"`
	Timezone     string     `json:"timezone"`
	Accept       bool       `json:"accept"`
	Acknowledged bool       `json:"acknowledged"`
}

// Requester is who started a connect, recorded with the acknowledgement.
type Requester struct {
	UserID    uuid.UUID
	IP        string
	UserAgent string
}

// ConnectSession is a connect or reconnect in progress; Account is set once it succeeded.
type ConnectSession struct {
	ID         string                `json:"id"`
	Kind       string                `json:"kind"`
	Channel    string                `json:"channel"`
	Method     *string               `json:"method"`
	Status     string                `json:"status"`
	Checkpoint *graphkit.Checkpoint  `json:"checkpoint"`
	CanResend  bool                  `json:"can_resend"`
	Account    *Account              `json:"account"`
	Error      *graphkit.SocialError `json:"error"`
	ExpiresAt  time.Time             `json:"expires_at"`
	CreatedAt  time.Time             `json:"created_at"`
}

// ConnectResult is the answer to POST /v1/social/connections.
type ConnectResult struct {
	Session       *ConnectSession `json:"session"`
	ChargedMicros int64           `json:"charged_micros"`
}

// seatChargeKey keys a seat's first month to its connect session, so a retry never charges twice.
func seatChargeKey(sessionID string) string { return "social:seat:" + sessionID }

// CreateConnection starts a connect at GraphKit. A new managed seat's first month is charged here and given
// back if the session ends without an account; a reconnect and a linked workspace's seat are never charged.
func (s *Service) CreateConnection(ctx context.Context, orgID uuid.UUID, who Requester, in ConnectRequest) (*ConnectResult, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	if !in.Acknowledged {
		return nil, errAcknowledgement
	}
	in.Timezone = strings.TrimSpace(in.Timezone)
	if in.Timezone != "" {
		if _, err := time.LoadLocation(in.Timezone); err != nil {
			return nil, invalid("timezone must be an IANA time zone such as Europe/London.")
		}
	}
	var reconnect *models.SocialAccount
	if in.AccountID != nil {
		a, err := s.live(ctx, orgID, *in.AccountID)
		if err != nil {
			return nil, err
		}
		reconnect = a
	}
	if reconnect != nil {
		in.Channel = reconnect.Channel
	}
	if _, ok := s.channelSpec(ctx, in.Channel); !ok {
		return nil, invalid("channel must be one of linkedin, instagram, whatsapp, telegram or facebook.")
	}
	ws, err := s.refreshWorkspace(ctx, orgID, 10*time.Second)
	if err != nil {
		return nil, s.upstream(ctx, "social.workspace", err)
	}
	linked := ws.Mode == graphkit.SocialModeLinked
	var due int64
	price, err := s.seatPrice(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if !linked && reconnect == nil {
		if err := s.requireBuyer(ctx, orgID); err != nil {
			return nil, err
		}
		if s.Store.Metered() && price.MonthlyMicros > 0 {
			due = price.MonthlyMicros
		}
		if due > 0 && !in.Accept {
			return nil, &AcceptRequiredError{Quote: mustJSON(map[string]any{
				"monthly_micros": price.MonthlyMicros, "currency": "usd", "billed_by": "warmbly",
			})}
		}
	}
	input := graphkit.ConnectInput{
		Channel: in.Channel, Timezone: in.Timezone, AcceptCharge: linked && in.Accept,
		Acknowledgement: &graphkit.Acknowledgement{Version: ws.AcknowledgementVersion, AcceptedAt: s.now(),
			PersonExternalID: who.UserID.String(), IP: who.IP, UserAgent: who.UserAgent},
	}
	if reconnect != nil {
		input.AccountID = &reconnect.GraphKitID
	}
	gs, err := s.api().CreateConnection(ctx, ext(orgID), input, opKey("connect"))
	if err != nil {
		return nil, s.upstream(ctx, "social.connect", err)
	}
	row := &models.SocialConnectSession{ID: gs.ID, OrganizationID: orgID, UserID: &who.UserID, Channel: in.Channel, Kind: gs.Kind, Status: gs.Status}
	if reconnect != nil {
		row.AccountID = &reconnect.ID
	}
	if due > 0 {
		key := seatChargeKey(gs.ID)
		row.ChargeKey, row.ChargedMicros = &key, models.Micro(due)
	}
	// The row comes first, so a charge that lands is always found and given back if the session fails.
	if err := s.Repo.CreateSession(ctx, row); err != nil {
		s.abandon(ctx, orgID, gs.ID)
		return nil, err
	}
	if due > 0 {
		key := *row.ChargeKey
		reason := models.SocialChannelLabel(in.Channel) + " seat · " + store.USD(due) + " · " + store.MonthLabel(s.now())
		err := s.Store.Collect(ctx, orgID, due, key, reason, false)
		if err == nil {
			_, err = s.Credits.ChargeAmount(ctx, orgID, models.Micro(due), reason, key)
		}
		if err != nil {
			bg := context.WithoutCancel(ctx)
			s.abandon(bg, orgID, gs.ID)
			_ = s.Repo.UpdateSession(bg, orgID, gs.ID, graphkit.ConnectCancelled, nil, nil)
			s.refundSession(bg, row)
			return nil, paymentError(err)
		}
	}
	out, err := s.observe(ctx, row, gs)
	if err != nil {
		return nil, err
	}
	return &ConnectResult{Session: out, ChargedMicros: due}, nil
}

// abandon cancels a GraphKit session nothing will use.
func (s *Service) abandon(ctx context.Context, orgID uuid.UUID, id string) {
	if _, err := s.api().CancelConnection(context.WithoutCancel(ctx), ext(orgID), id, opKey("connect.cancel")); err != nil {
		log.Warn().Err(err).Str("org_id", orgID.String()).Str("session_id", id).Msg("social: cancelling an unused connect session failed; it expires on its own")
	}
}

// session is the workspace's connect session row, checked before anything is sent to GraphKit.
func (s *Service) session(ctx context.Context, orgID uuid.UUID, id string) (*models.SocialConnectSession, error) {
	if !strings.HasPrefix(id, "scs_") || len(id) > 100 {
		return nil, errNotFound
	}
	row, err := s.Repo.GetSession(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, errNotFound
	}
	return row, nil
}

// GetConnection is a connect session's current state.
func (s *Service) GetConnection(ctx context.Context, orgID uuid.UUID, id string) (*ConnectSession, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	row, err := s.session(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	gs, err := s.api().GetConnection(ctx, ext(orgID), id)
	if err != nil {
		return nil, s.upstream(ctx, "social.connect.get", err)
	}
	return s.observe(ctx, row, gs)
}

// SignInRequest is POST /v1/social/connections/:id/sign-in. The secrets in it are passed to GraphKit once and never kept.
type SignInRequest struct {
	Method            string            `json:"method"`
	Username          string            `json:"username"`
	Password          string            `json:"password"`
	Cookies           map[string]string `json:"cookies"`
	PhoneNumber       string            `json:"phone_number"`
	TwoFactorPassword string            `json:"two_factor_password"`
	Recruiter         bool              `json:"recruiter"`
	Country           string            `json:"country"`
}

// SignIn passes the account's sign-in to GraphKit. Only the fields the method uses may be sent.
func (s *Service) SignIn(ctx context.Context, orgID uuid.UUID, id string, who Requester, in SignInRequest) (*ConnectSession, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	row, err := s.session(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	spec, _ := s.channelSpec(ctx, row.Channel)
	if !offers(spec.AuthMethods, in.Method) {
		return nil, invalid(spec.Label + " signs in with " + strings.Join(spec.AuthMethods, " or ") + ".")
	}
	input := graphkit.SignInInput{Method: in.Method, UserAgent: who.UserAgent, IP: who.IP, Country: strings.ToUpper(strings.TrimSpace(in.Country))}
	username, phone := strings.TrimSpace(in.Username), strings.TrimSpace(in.PhoneNumber)
	extra := func(sent bool, field string) error {
		if sent {
			return invalid(field + " is not used by the " + in.Method + " sign-in.")
		}
		return nil
	}
	var refuse error
	switch in.Method {
	case graphkit.SignInPassword:
		if username == "" || in.Password == "" {
			return nil, invalid("Enter the account's username, email or phone and its password.")
		}
		input.Username, input.Password, input.Recruiter = username, in.Password, in.Recruiter && row.Channel == models.SocialLinkedIn
		refuse = errors.Join(extra(len(in.Cookies) > 0, "cookies"), extra(phone != "", "phone_number"))
	case graphkit.SignInCookie:
		cookies, err := signInCookies(spec, in.Cookies)
		if err != nil {
			return nil, err
		}
		input.Cookies, input.Recruiter = cookies, in.Recruiter && row.Channel == models.SocialLinkedIn
		refuse = errors.Join(extra(username != "" || in.Password != "", "username and password"), extra(phone != "", "phone_number"))
	case graphkit.SignInQR:
		refuse = errors.Join(extra(username != "" || in.Password != "", "username and password"), extra(len(in.Cookies) > 0, "cookies"),
			extra(phone != "", "phone_number"))
	case graphkit.SignInPairingCode:
		if input.PhoneNumber = models.E164(phone); input.PhoneNumber == "" {
			return nil, invalid("phone_number must be the account's number with its country code, such as +447700900123.")
		}
		refuse = errors.Join(extra(username != "" || in.Password != "", "username and password"), extra(len(in.Cookies) > 0, "cookies"))
	}
	if refuse == nil && in.TwoFactorPassword != "" {
		if row.Channel != models.SocialTelegram {
			refuse = invalid("two_factor_password is only used by Telegram.")
		}
		input.TwoFactorPassword = in.TwoFactorPassword
	}
	if refuse != nil {
		var xe *errx.Error
		if errors.As(refuse, &xe) {
			return nil, xe
		}
		return nil, refuse
	}
	if err := s.Repo.SetSessionMethod(ctx, orgID, id, in.Method); err != nil {
		return nil, err
	}
	m := in.Method
	row.Method = &m
	gs, err := s.api().SignIn(ctx, ext(orgID), id, input, opKey("connect.sign_in"))
	if err != nil {
		return nil, s.upstream(ctx, "social.connect.sign_in", err)
	}
	return s.observe(ctx, row, gs)
}

// signInCookies checks a cookie sign-in carries the channel's required cookies and no others.
func signInCookies(spec graphkit.SocialChannel, in map[string]string) (map[string]string, error) {
	if spec.CookieNames == nil {
		return nil, invalid(spec.Label + " does not sign in with cookies.")
	}
	out := map[string]string{}
	for name, v := range in {
		if !offers(spec.CookieNames.Required, name) && !offers(spec.CookieNames.Optional, name) {
			return nil, invalid(name + " is not a " + spec.Label + " sign-in cookie.")
		}
		if v = strings.TrimSpace(v); v != "" {
			out[name] = v
		}
	}
	for _, name := range spec.CookieNames.Required {
		if out[name] == "" {
			return nil, invalid("Paste the account's " + name + " cookie.")
		}
	}
	return out, nil
}

// Answer answers a checkpoint: a code, one choice or several.
func (s *Service) Answer(ctx context.Context, orgID uuid.UUID, id string, in graphkit.CheckpointAnswer) (*ConnectSession, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	row, err := s.session(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	in.Code, in.Choice, in.Secret = strings.TrimSpace(in.Code), strings.TrimSpace(in.Choice), strings.TrimSpace(in.Secret)
	if in.Code == "" && in.Choice == "" && len(in.Choices) == 0 && in.TwoFactorPassword == "" {
		return nil, invalid("Send a code, a choice, choices or the two-factor password.")
	}
	if in.Secret != "" && in.Choice == "" {
		return nil, invalid("secret goes with a choice, such as PIN or RECOVERY_CODE.")
	}
	gs, err := s.api().AnswerCheckpoint(ctx, ext(orgID), id, in, opKey("connect.answer"))
	if err != nil {
		return nil, s.upstream(ctx, "social.connect.answer", err)
	}
	return s.observe(ctx, row, gs)
}

// ChooseMethod picks how a checkpoint's code is delivered.
func (s *Service) ChooseMethod(ctx context.Context, orgID uuid.UUID, id, challenge string) (*ConnectSession, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	row, err := s.session(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if challenge = strings.TrimSpace(challenge); challenge == "" || len(challenge) > 64 {
		return nil, invalid("challenge names how the code is sent.")
	}
	gs, err := s.api().ChooseMethod(ctx, ext(orgID), id, challenge, opKey("connect.method"))
	if err != nil {
		return nil, s.upstream(ctx, "social.connect.method", err)
	}
	return s.observe(ctx, row, gs)
}

// Resend sends a checkpoint's code again.
func (s *Service) Resend(ctx context.Context, orgID uuid.UUID, id string) (*ConnectSession, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	row, err := s.session(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	gs, err := s.api().ResendCheckpoint(ctx, ext(orgID), id, opKey("connect.resend"))
	if err != nil {
		return nil, s.upstream(ctx, "social.connect.resend", err)
	}
	return s.observe(ctx, row, gs)
}

// CancelConnection cancels a connect; a first month it charged is given back.
func (s *Service) CancelConnection(ctx context.Context, orgID uuid.UUID, id string) error {
	if err := s.ready(ctx, orgID); err != nil {
		return err
	}
	row, err := s.session(ctx, orgID, id)
	if err != nil {
		return err
	}
	gs, err := s.api().CancelConnection(ctx, ext(orgID), id, opKey("connect.cancel"))
	if err != nil {
		if ge, ok := graphkit.AsError(err); ok && (ge.Code == graphkit.CodeSessionExpired || ge.Code == graphkit.CodeStateConflict) {
			gs, err = s.api().GetConnection(ctx, ext(orgID), id)
		}
		if err != nil {
			return s.upstream(ctx, "social.connect.cancel", err)
		}
	}
	_, err = s.observe(ctx, row, gs)
	return err
}

// observe records what GraphKit says of a session: the account it connected, the seat it starts, a refund it owes.
func (s *Service) observe(ctx context.Context, row *models.SocialConnectSession, gs *graphkit.ConnectSession) (*ConnectSession, error) {
	orgID := row.OrganizationID
	var acct *models.SocialAccount
	if gs.Account != nil {
		// Only the connect itself revives a removed account, never a later read of a session already settled.
		record := s.record
		if gs.Status == graphkit.ConnectSucceeded && row.AccountID == nil {
			record = s.recordConnected
		}
		a, err := record(ctx, orgID, gs.Account, row.UserID)
		if err != nil {
			return nil, err
		}
		acct = a
	}
	// A seat whose month did not start keeps the session unsettled, so the reconciler tries again.
	settled := true
	if gs.Status == graphkit.ConnectSucceeded && acct != nil {
		got, ok := s.startSeat(ctx, row, acct)
		if got != nil {
			acct = got
		}
		settled = ok
	}
	if gs.Status != row.Status || (acct != nil && settled && row.AccountID == nil) {
		var gkID *string
		var accountID *uuid.UUID
		if acct != nil && settled {
			gkID, accountID = &acct.GraphKitID, &acct.ID
		}
		if err := s.Repo.UpdateSession(ctx, orgID, row.ID, gs.Status, gkID, accountID); err != nil {
			return nil, err
		}
		row.Status = gs.Status
		if accountID != nil {
			row.AccountID = accountID
		}
		s.publish(ctx, orgID, pubsub.SocialEvent{BaseEvent: pubsub.BaseEvent{EventType: pubsub.EventSocialConnectionUpdated},
			Channel: row.Channel, SessionID: row.ID, Status: gs.Status})
	}
	if gs.Ended() && gs.Status != graphkit.ConnectSucceeded {
		s.refundSession(ctx, row)
	}
	channel := gs.Channel
	if channel == "" {
		channel = row.Channel
	}
	return &ConnectSession{ID: gs.ID, Kind: gs.Kind, Channel: channel, Method: row.Method, Status: gs.Status, Checkpoint: gs.Checkpoint,
		CanResend: gs.CanResend, Account: toAccount(acct), Error: gs.Error, ExpiresAt: gs.ExpiresAt, CreatedAt: gs.CreatedAt}, nil
}

// startSeat starts the month a connect paid for. An account already paid ahead gives the new charge back;
// a charge that never landed leaves the seat due at once, so the next billing run collects it.
func (s *Service) startSeat(ctx context.Context, row *models.SocialConnectSession, acct *models.SocialAccount) (*models.SocialAccount, bool) {
	if row.Kind != graphkit.ConnectKindConnect || acct.BillingMode != models.SocialBillingStore {
		return nil, true
	}
	orgID := row.OrganizationID
	if row.ChargeKey != nil && acct.PaidKey != nil && *acct.PaidKey == *row.ChargeKey {
		return nil, true
	}
	if acct.PaidThrough != nil && acct.PaidThrough.After(s.now()) {
		s.refundSession(ctx, row)
		return nil, true
	}
	monthly := models.Micro(0)
	if s.Store.Metered() {
		price, err := s.seatPrice(ctx, orgID)
		if err != nil {
			log.Warn().Err(err).Str("session_id", row.ID).Msg("social: pricing a seat's month failed; retried on the next pass")
			return nil, false
		}
		monthly = models.Micro(price.MonthlyMicros)
	}
	// The month starts now (a paid month that already ran out is behind it), so it never lands in the past.
	start := s.now()
	through := addMonth(start)
	var paidKey *string
	var paid models.Micro
	if row.ChargeKey != nil && row.ChargedMicros > 0 {
		charge, err := s.Credits.ChargeOf(ctx, orgID, *row.ChargeKey)
		if err != nil {
			log.Warn().Err(err).Str("session_id", row.ID).Msg("social: reading a seat's first charge failed; retried on the next pass")
			return nil, false
		}
		if charge != nil {
			paidKey, paid, monthly = row.ChargeKey, row.ChargedMicros, row.ChargedMicros
		} else {
			through = s.now()
		}
	}
	if err := s.Repo.SetSeatBilling(ctx, orgID, acct.ID, models.SocialBillingStore, monthly, &through, paidKey, paid); err != nil {
		log.Warn().Err(err).Str("account_id", acct.ID.String()).Msg("social: starting a seat's month failed; retried on the next pass")
		return nil, false
	}
	got, err := s.Repo.GetAccount(ctx, orgID, acct.ID)
	if err != nil || got == nil {
		return nil, true
	}
	s.publishAccount(ctx, got)
	return got, true
}

// refundSession gives back once the first month a connect took when it ended without its account.
func (s *Service) refundSession(ctx context.Context, row *models.SocialConnectSession) {
	if row.ChargeKey == nil || row.ChargedMicros <= 0 || row.RefundedAt != nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	key := *row.ChargeKey
	if _, err := s.Credits.RefundStore(ctx, row.OrganizationID, key, key, key+":refund", 0, "Refund · "+models.SocialChannelLabel(row.Channel)+" seat · connection not completed"); err != nil {
		log.Error().Err(err).Str("session_id", row.ID).Msg("social: refunding a connect failed; the reconciler retries")
		return
	}
	if _, err := s.Repo.MarkSessionRefunded(ctx, row.OrganizationID, row.ID); err != nil {
		log.Warn().Err(err).Str("session_id", row.ID).Msg("social: recording a connect refund failed")
		return
	}
	t := s.now()
	row.RefundedAt = &t
}

// addMonth is one calendar month on, clamped to the month's last day like Postgres's interval '1 month'.
func addMonth(t time.Time) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+1, 1, t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), t.Location())
	last := first.AddDate(0, 1, -1).Day()
	return first.AddDate(0, 0, min(d, last)-1)
}
