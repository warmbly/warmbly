package graphkit

import (
	"context"
	"hash/fnv"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The social sandbox: simulated LinkedIn accounts whose sign-in username decides the outcome
// (2fa, approve, contract, wrong, restricted, salesnav, recruiter), deterministic search, and
// invitations to public ids containing accept (and reply) that are accepted (and answered).

// FakeAcknowledgementVersion is the terms version the sandbox asks connect sessions to accept.
const FakeAcknowledgementVersion = "2026-11-15"

const (
	fakeConnectTTL     = 20 * time.Minute
	fakeLinkTTL        = time.Hour
	fakeMaxSignIns     = 5
	fakeMaxAnswers     = 10
	fakeResendGap      = 30 * time.Second
	fakeApproveAfter   = 5 * time.Second
	fakeRestrictAfter  = time.Minute
	fakeActionAfter    = 5 * time.Second
	fakeAcceptAfter    = time.Minute
	fakeReplyAfter     = time.Minute
	fakeMessageSendIn  = 2 * time.Second
	fakeFailedSignInDy = 100
	fakeAIPerDay       = 200
	fakeLinkedSeatCap  = 5
)

type fakeSocial struct {
	workspaces map[string]*fakeSocialWorkspace
	links      map[string]*fakeLink
	sessions   map[string]*fakeConnect
	accounts   map[string]*fakeSocialAccount
	actions    map[string]*fakeAction
	convs      map[string]*fakeConv
	messages   map[string][]*fakeMessage
	// connected is account id + public id for people who accepted an invitation.
	connected map[string]bool
	// failedSignIns and aiBuilds count per external id and UTC day.
	failedSignIns map[string]int
	aiBuilds      map[string]int
}

type fakeSocialWorkspace struct {
	mode     string
	name     string
	linkedAt *time.Time
}

type fakeLink struct {
	external  string
	s         LinkSession
	returnURL string
}

type fakeConnect struct {
	external   string
	s          ConnectSession
	signIns    int
	answers    int
	username   string
	recruiter  bool
	timezone   string
	approveAt  *time.Time
	lastResend time.Time
	// reconnect is the account a reconnect session signs in again.
	reconnect string
}

type fakeSocialAccount struct {
	external   string
	a          SocialAccount
	restrictAt *time.Time
	searchDay  string
	searchUsed int
}

type fakeAction struct {
	external string
	a        Action
	runAt    time.Time
	acceptAt *time.Time
	replyAt  *time.Time
}

type fakeConv struct {
	external string
	c        Conversation
	replyAt  *time.Time
}

type fakeMessage struct {
	m      SocialMessage
	sendAt *time.Time
}

func newFakeSocial() *fakeSocial {
	return &fakeSocial{
		workspaces: map[string]*fakeSocialWorkspace{}, links: map[string]*fakeLink{}, sessions: map[string]*fakeConnect{},
		accounts: map[string]*fakeSocialAccount{}, actions: map[string]*fakeAction{}, convs: map[string]*fakeConv{},
		messages: map[string][]*fakeMessage{}, connected: map[string]bool{}, failedSignIns: map[string]int{}, aiBuilds: map[string]int{},
	}
}

// SetLinkPage makes link session URLs point at a stand-in consent page served by the caller.
func (f *Fake) SetLinkPage(url func(sessionID string) string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.linkPage = url
}

// LinkSessionReturn is where the stand-in consent page sends the browser for a pending session.
func (f *Fake) LinkSessionReturn(sessionID string) (returnURL string, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.social.links[sessionID]
	if l == nil || l.s.Status != LinkPending || !f.now().Before(l.s.ExpiresAt) {
		return "", false
	}
	return l.returnURL, true
}

// DecideLink is the stand-in consent page's Link or Cancel; it returns the URL to send the browser to.
func (f *Fake) DecideLink(sessionID string, approve bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.social.links[sessionID]
	now := f.now().UTC()
	if l == nil {
		return "", fakeErr(http.StatusNotFound, CodeNotFound, "no such link session")
	}
	if l.s.Status != LinkPending || !now.Before(l.s.ExpiresAt) {
		return "", fakeErr(http.StatusConflict, CodeStateConflict, "this link session is no longer pending")
	}
	status := LinkCancelled
	if approve {
		if f.managedLiveLocked(l.external) {
			return "", fakeErr(http.StatusConflict, CodeSocialModeConflict, "remove the managed accounts before linking")
		}
		status = LinkLinked
		name := "Sample workspace"
		l.s.Workspace = &WorkspaceName{Name: name}
		ws := f.workspaceLocked(l.external)
		ws.mode, ws.name, ws.linkedAt = SocialModeLinked, name, &now
		// The linked workspace brings the account its owner connected in GraphKit's app.
		f.connectAccountLocked(l.external, "", "grace.hopper.salesnav", false, "Europe/London", now)
		f.emitLocked(EventSocialUpdated, l.external, f.workspaceViewLocked(l.external))
	}
	l.s.Status = status
	sep := "?"
	if strings.Contains(l.returnURL, "?") {
		sep = "&"
	}
	return l.returnURL + sep + "link_session=" + l.s.ID + "&status=" + status, nil
}

// RevokeLink is the customer revoking the link from GraphKit's Connected apps.
func (f *Fake) RevokeLink(externalID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ws := f.social.workspaces[externalID]; ws != nil && ws.mode == SocialModeLinked {
		f.unlinkLocked(externalID)
	}
}

func (f *Fake) workspaceLocked(external string) *fakeSocialWorkspace {
	ws := f.social.workspaces[external]
	if ws == nil {
		ws = &fakeSocialWorkspace{mode: SocialModeNone}
		f.social.workspaces[external] = ws
	}
	return ws
}

func (f *Fake) managedLiveLocked(external string) bool {
	ws := f.social.workspaces[external]
	if ws == nil || ws.mode != SocialModeManaged {
		return false
	}
	for _, a := range f.social.accounts {
		if a.external == external && a.a.Status != SocialRemoved {
			return true
		}
	}
	return false
}

func (f *Fake) workspaceViewLocked(external string) SocialWorkspace {
	ws := f.workspaceLocked(external)
	out := SocialWorkspace{Mode: ws.mode, LinkedAt: ws.linkedAt, AcknowledgementVersion: FakeAcknowledgementVersion, Channels: []string{ChannelLinkedIn}}
	if ws.mode == SocialModeLinked {
		out.Workspace = &WorkspaceName{Name: ws.name}
	}
	for _, a := range f.social.accounts {
		if a.external == external && a.a.Status != SocialRemoved {
			out.Accounts++
		}
	}
	return out
}

func (f *Fake) socialAccountLocked(externalID string) error {
	if err := f.refusedLocked(); err != nil {
		return err
	}
	if f.accounts[externalID] == nil {
		return fakeErr(http.StatusNotFound, CodeAccountNotFound, "no such account")
	}
	return nil
}

func (f *Fake) SocialWorkspace(_ context.Context, externalID string) (*SocialWorkspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	return cloneJSON(f.workspaceViewLocked(externalID)), nil
}

func (f *Fake) CreateLinkSession(_ context.Context, externalID, returnURL, key string) (*LinkSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:link:"+externalID, returnURL, func() (*LinkSession, error) {
		if err := f.socialAccountLocked(externalID); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(returnURL, "https://") && !strings.HasPrefix(returnURL, "http://") {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "return_url must be an absolute URL", map[string]any{"fields": []string{"return_url"}})
		}
		if f.managedLiveLocked(externalID) {
			return nil, fakeErr(http.StatusConflict, CodeSocialModeConflict, "remove the managed accounts before linking")
		}
		id := f.id("sls")
		u := "https://app.graphkit.com/partner-link/" + id
		if f.linkPage != nil {
			u = f.linkPage(id)
		}
		l := &fakeLink{external: externalID, returnURL: returnURL,
			s: LinkSession{ID: id, URL: u, Status: LinkPending, ExpiresAt: f.now().UTC().Add(fakeLinkTTL)}}
		f.social.links[id] = l
		return cloneJSON(l.s), nil
	})
}

func (f *Fake) GetLinkSession(_ context.Context, externalID, id string) (*LinkSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	l := f.social.links[id]
	if l == nil || l.external != externalID {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such link session")
	}
	if l.s.Status == LinkPending && !f.now().Before(l.s.ExpiresAt) {
		l.s.Status = LinkExpired
	}
	out := cloneJSON(l.s)
	out.URL = ""
	return out, nil
}

func (f *Fake) Unlink(_ context.Context, externalID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := replay(f, key, "social:unlink:"+externalID, nil, func() (*struct{}, error) {
		if err := f.socialAccountLocked(externalID); err != nil {
			return nil, err
		}
		if ws := f.social.workspaces[externalID]; ws != nil && ws.mode == SocialModeLinked {
			f.unlinkLocked(externalID)
		}
		return &struct{}{}, nil
	})
	return err
}

// unlinkLocked leaves the linked workspace's accounts in GraphKit; they are no longer the partner's.
func (f *Fake) unlinkLocked(external string) {
	ws := f.workspaceLocked(external)
	ws.mode, ws.name, ws.linkedAt = SocialModeNone, "", nil
	for id, a := range f.social.accounts {
		if a.external == external {
			delete(f.social.accounts, id)
		}
	}
	f.emitLocked(EventSocialUpdated, external, f.workspaceViewLocked(external))
}

// ---- connect sessions ---------------------------------------------------

func (f *Fake) sessionLocked(externalID, id string) (*fakeConnect, error) {
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	s := f.social.sessions[id]
	if s == nil || s.external != externalID {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such connect session")
	}
	f.expireSessionLocked(s, f.now().UTC())
	return s, nil
}

func (f *Fake) expireSessionLocked(s *fakeConnect, now time.Time) bool {
	if s.s.Ended() || now.Before(s.s.ExpiresAt) {
		return false
	}
	s.s.Status, s.s.Checkpoint, s.s.CanResend = ConnectExpired, nil, false
	f.emitLocked(EventSocialConnectionUpdated, s.external, s.s)
	return true
}

func (f *Fake) openSessionLocked(externalID, id string) (*fakeConnect, error) {
	s, err := f.sessionLocked(externalID, id)
	if err != nil {
		return nil, err
	}
	switch s.s.Status {
	case ConnectExpired:
		return nil, fakeErr(http.StatusConflict, CodeSessionExpired, "this connect session expired; start a new one")
	case ConnectSucceeded, ConnectFailed, ConnectCancelled:
		return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this connect session has ended")
	}
	return s, nil
}

func (f *Fake) CreateConnection(_ context.Context, externalID string, in ConnectInput, key string) (*ConnectSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:connect:"+externalID, in, func() (*ConnectSession, error) {
		if err := f.socialAccountLocked(externalID); err != nil {
			return nil, err
		}
		if in.Channel != ChannelLinkedIn {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "channel must be linkedin", map[string]any{"fields": []string{"channel"}})
		}
		tz := in.Timezone
		if tz == "" {
			tz = "UTC"
		}
		if _, err := time.LoadLocation(tz); err != nil {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "unknown timezone", map[string]any{"fields": []string{"timezone"}})
		}
		kind, reconnect := ConnectKindConnect, ""
		if in.AccountID != nil && *in.AccountID != "" {
			a := f.social.accounts[*in.AccountID]
			if a == nil || a.external != externalID {
				return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such social account")
			}
			if a.a.Status == SocialRemoved {
				return nil, fakeErr(http.StatusConflict, CodeAccountUnavailable, "this account was removed")
			}
			kind, reconnect = ConnectKindReconnect, a.a.ID
		}
		if in.Acknowledgement == nil || in.Acknowledgement.Version != FakeAcknowledgementVersion || in.Acknowledgement.PersonExternalID == "" {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "acknowledgement must accept version "+FakeAcknowledgementVersion,
				map[string]any{"fields": []string{"acknowledgement"}})
		}
		ws := f.workspaceLocked(externalID)
		if ws.mode == SocialModeLinked && kind == ConnectKindConnect {
			if f.workspaceViewLocked(externalID).Accounts >= fakeLinkedSeatCap {
				return nil, fakeErrDetails(http.StatusPaymentRequired, CodeSocialCheckoutRequired, "the linked workspace's plan has no seat left",
					map[string]any{"url": "https://app.graphkit.com/settings/billing"})
			}
			if !in.AcceptCharge {
				return nil, fakeErrDetails(http.StatusConflict, CodeStateConflict, "the linked workspace's plan will be charged for this seat",
					map[string]any{"quote": map[string]any{"monthly_micros": 39_000_000, "currency": "USD", "billed_by": "linked_workspace"}})
			}
		}
		if ws.mode == SocialModeNone {
			ws.mode = SocialModeManaged
			f.emitLocked(EventSocialUpdated, externalID, f.workspaceViewLocked(externalID))
		}
		now := f.now().UTC()
		s := &fakeConnect{external: externalID, timezone: tz, reconnect: reconnect,
			s: ConnectSession{ID: f.id("scs"), Kind: kind, Channel: ChannelLinkedIn, Status: ConnectSignIn, ExpiresAt: now.Add(fakeConnectTTL), CreatedAt: now}}
		f.social.sessions[s.s.ID] = s
		f.emitLocked(EventSocialConnectionUpdated, externalID, s.s)
		return cloneJSON(s.s), nil
	})
}

func (f *Fake) GetConnection(_ context.Context, externalID, id string) (*ConnectSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.sessionLocked(externalID, id)
	if err != nil {
		return nil, err
	}
	return cloneJSON(s.s), nil
}

// SignIn is never replayed: credentials are not fingerprinted or kept.
func (f *Fake) SignIn(_ context.Context, externalID, id string, in SignInInput, _ string) (*ConnectSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.openSessionLocked(externalID, id)
	if err != nil {
		return nil, err
	}
	if s.s.Status != ConnectSignIn {
		return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this session is waiting for a checkpoint answer")
	}
	now := f.now().UTC()
	day := externalID + now.Format("2006-01-02")
	if f.social.failedSignIns[day] >= fakeFailedSignInDy {
		e := fakeErr(http.StatusTooManyRequests, CodeTooManyFailedSignIns, "too many refused sign-ins today")
		e.RetryAfter = time.Hour
		return nil, e
	}
	var user string
	switch in.Method {
	case "password":
		if strings.TrimSpace(in.Username) == "" || in.Password == "" {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "username and password are required", map[string]any{"fields": []string{"username", "password"}})
		}
		user = strings.ToLower(in.Username)
		if len(in.Password) < 8 {
			user = "wrong"
		}
	case "cookie":
		if in.Cookies == nil || strings.TrimSpace(in.Cookies.LiAt) == "" {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "cookies.li_at is required", map[string]any{"fields": []string{"cookies.li_at"}})
		}
		user = strings.ToLower(in.Cookies.LiAt)
	default:
		return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "method must be password or cookie", map[string]any{"fields": []string{"method"}})
	}
	s.signIns++
	if strings.Contains(user, "wrong") {
		f.social.failedSignIns[day]++
		if s.signIns >= fakeMaxSignIns {
			f.failSessionLocked(s, CodeSignInFailed, "LinkedIn refused the sign-in too many times")
		}
		return nil, fakeErr(http.StatusUnprocessableEntity, CodeSignInFailed, "LinkedIn refused the email or password")
	}
	s.username, s.recruiter = user, in.Recruiter || strings.Contains(user, "recruiter")
	switch {
	case strings.Contains(user, "2fa"):
		f.checkpointLocked(s, CheckpointTwoFactor, "sms", nil)
	case strings.Contains(user, "approve"):
		at := now.Add(fakeApproveAfter)
		s.approveAt = &at
		f.checkpointLocked(s, CheckpointAppApproval, "", nil)
	case strings.Contains(user, "contract"):
		f.checkpointLocked(s, CheckpointChooseContract, "", []CheckpointChoice{
			{ID: "contract_sales_navigator", Label: "Sales Navigator Core", Description: "Acme sales team"},
			{ID: "contract_recruiter", Label: "Recruiter Lite", Description: "Acme talent team"},
		})
	default:
		f.succeedLocked(s, now)
	}
	return cloneJSON(s.s), nil
}

func (f *Fake) checkpointLocked(s *fakeConnect, typ, delivery string, choices []CheckpointChoice) {
	attempts := 4
	cp := &Checkpoint{Type: typ, Choices: choices, AttemptsRemaining: &attempts}
	if cp.Choices == nil {
		cp.Choices = []CheckpointChoice{}
	}
	if delivery != "" {
		cp.Delivery = &delivery
	}
	s.s.Status, s.s.Checkpoint = ConnectCheckpoint, cp
	s.s.CanResend = typ == CheckpointTwoFactor || typ == CheckpointOTP || typ == CheckpointOTPOrApp
	f.emitLocked(EventSocialConnectionUpdated, s.external, s.s)
}

func (f *Fake) failSessionLocked(s *fakeConnect, code, msg string) {
	s.s.Status, s.s.Checkpoint, s.s.CanResend = ConnectFailed, nil, false
	s.s.Error = &SocialError{Code: code, Message: msg}
	f.emitLocked(EventSocialConnectionUpdated, s.external, s.s)
}

func (f *Fake) succeedLocked(s *fakeConnect, now time.Time) {
	a := f.connectAccountLocked(s.external, s.reconnect, s.username, s.recruiter, s.timezone, now)
	s.s.Status, s.s.Checkpoint, s.s.CanResend, s.s.Account = ConnectSucceeded, nil, false, cloneJSON(a.a)
	f.emitLocked(EventSocialConnectionUpdated, s.external, s.s)
}

func (f *Fake) AnswerCheckpoint(_ context.Context, externalID, id string, in CheckpointAnswer, _ string) (*ConnectSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.openSessionLocked(externalID, id)
	if err != nil {
		return nil, err
	}
	if s.s.Status != ConnectCheckpoint || s.s.Checkpoint == nil {
		return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this session is not at a checkpoint")
	}
	s.answers++
	if s.answers > fakeMaxAnswers {
		f.failSessionLocked(s, CodeSignInFailed, "too many checkpoint answers")
		return cloneJSON(s.s), nil
	}
	cp, now := s.s.Checkpoint, f.now().UTC()
	switch cp.Type {
	case CheckpointTwoFactor, CheckpointOTP, CheckpointOTPOrApp:
		if strings.TrimSpace(in.Code) == "" {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "code is required", map[string]any{"fields": []string{"code"}})
		}
		if strings.TrimSpace(in.Code) == "123456" {
			f.succeedLocked(s, now)
			break
		}
		left := *cp.AttemptsRemaining - 1
		msg := "invalid_code"
		cp.AttemptsRemaining, cp.LastError = &left, &msg
		if left <= 0 {
			f.failSessionLocked(s, CodeSignInFailed, "the verification code was refused too many times")
			break
		}
		f.emitLocked(EventSocialConnectionUpdated, s.external, s.s)
	case CheckpointChooseContract:
		switch in.Choice {
		case "contract_sales_navigator":
			s.recruiter = false
			s.username += " salesnav"
		case "contract_recruiter":
			s.recruiter = true
		default:
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "choice must be one of the checkpoint's choices", map[string]any{"fields": []string{"choice"}})
		}
		f.succeedLocked(s, now)
	case CheckpointAppApproval:
		return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this checkpoint completes on its own once approved in the LinkedIn app")
	default:
		return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this checkpoint takes no answer")
	}
	return cloneJSON(s.s), nil
}

func (f *Fake) ChooseMethod(_ context.Context, externalID, id, challenge, key string) (*ConnectSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:method:"+id, challenge, func() (*ConnectSession, error) {
		s, err := f.openSessionLocked(externalID, id)
		if err != nil {
			return nil, err
		}
		if s.s.Checkpoint == nil || s.s.Checkpoint.Type != CheckpointChooseMethod {
			return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this session is not choosing a method")
		}
		f.checkpointLocked(s, CheckpointTwoFactor, challenge, nil)
		return cloneJSON(s.s), nil
	})
}

func (f *Fake) ResendCheckpoint(_ context.Context, externalID, id, key string) (*ConnectSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:resend:"+id, nil, func() (*ConnectSession, error) {
		s, err := f.openSessionLocked(externalID, id)
		if err != nil {
			return nil, err
		}
		if !s.s.CanResend {
			return nil, fakeErr(http.StatusConflict, CodeStateConflict, "this checkpoint has nothing to resend")
		}
		now := f.now().UTC()
		if wait := s.lastResend.Add(fakeResendGap).Sub(now); wait > 0 {
			e := fakeErr(http.StatusTooManyRequests, CodeRateLimited, "resends are 30 seconds apart")
			e.RetryAfter = wait
			return nil, e
		}
		s.lastResend = now
		return cloneJSON(s.s), nil
	})
}

func (f *Fake) CancelConnection(_ context.Context, externalID, id, key string) (*ConnectSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:cancel:"+id, nil, func() (*ConnectSession, error) {
		s, err := f.sessionLocked(externalID, id)
		if err != nil {
			return nil, err
		}
		if !s.s.Ended() {
			s.s.Status, s.s.Checkpoint, s.s.CanResend = ConnectCancelled, nil, false
			f.emitLocked(EventSocialConnectionUpdated, externalID, s.s)
		}
		return cloneJSON(s.s), nil
	})
}

// ---- accounts -----------------------------------------------------------

var fakeFirstNames = []string{"Ada", "Grace", "Maya", "Liam", "Noah", "Ines", "Kai", "Zara", "Omar", "Lena", "Theo", "Priya", "Jonas", "Elif", "Sam", "Rosa"}
var fakeLastNames = []string{"Lovelace", "Hopper", "Chen", "Okafor", "Silva", "Novak", "Haddad", "Lindqvist", "Moreau", "Kowalski", "Tanaka", "Reyes", "Mensah", "Fischer", "Rivera", "Dubois"}

func fakeHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func fakeInt(v int) *int { return &v }

func (f *Fake) connectAccountLocked(external, reconnect, user string, recruiter bool, tz string, now time.Time) *fakeSocialAccount {
	if a := f.social.accounts[reconnect]; a != nil {
		a.a.Status, a.a.StatusReason, a.a.UpdatedAt = SocialActive, nil, now
		a.a.Licences = fakeLicences(user, recruiter)
		f.emitLocked(EventSocialAccountUpdated, external, a.a)
		return a
	}
	h := fakeHash(user)
	first, last := fakeFirstNames[h%uint64(len(fakeFirstNames))], fakeLastNames[(h/7)%uint64(len(fakeLastNames))]
	if strings.HasPrefix(user, "grace.hopper") {
		first, last = "Grace", "Hopper"
	}
	public := strings.ToLower(first + "-" + last + "-" + strconv.FormatUint(h%9000+1000, 10))
	licences := fakeLicences(user, recruiter)
	searchLimit, searchCeil := 1000, 1000
	if slices.Contains(licences, LicenceSalesNavigator) {
		searchLimit, searchCeil = 1000, 2500
	}
	mode := f.workspaceLocked(external).mode
	billing := SocialBilling{Mode: SocialModeManaged, MonthlyMicros: new(int64)}
	if mode == SocialModeManaged {
		end := now.AddDate(0, 1, 0)
		billing.CurrentPeriodEnd = &end
	} else {
		billing = SocialBilling{Mode: SocialModeLinked}
	}
	a := &fakeSocialAccount{external: external, a: SocialAccount{
		ID: f.id("sac"), Channel: ChannelLinkedIn, Status: SocialActive,
		DisplayName: first + " " + last, Headline: "Head of Growth at " + last + " Labs",
		ProfileURL: "https://www.linkedin.com/in/" + public, PublicID: public,
		Licences:      licences,
		InmailCredits: &InmailCredits{Classic: fakeInt(0)},
		Limits:        SocialLimits{Invite: fakeInt(15), Message: fakeInt(50), Inmail: fakeInt(20), ProfileVisit: fakeInt(40), Search: fakeInt(searchLimit)},
		WeeklyLimits:  WeeklyLimits{Invite: fakeInt(80)},
		LimitCeilings: LimitCeilings{Invite: 25, Message: 100, Inmail: 40, ProfileVisit: 80, Search: searchCeil, WeeklyInvite: 100},
		WorkingHours:  &WorkingHours{Days: []int{1, 2, 3, 4, 5}, Start: "09:00", End: "18:00"}, Timezone: tz,
		Warmup:         &SocialWarmup{Enabled: true, Percent: fakeInt(20), EndsOn: ptr(now.AddDate(0, 0, 21).Format("2006-01-02"))},
		PendingInvites: fakeInt(0), AcceptanceRate: nil, WithdrawInvitesAfterDays: fakeInt(21),
		Billing: billing, ConnectedAt: &now, UpdatedAt: now,
	}}
	if slices.Contains(licences, LicenceSalesNavigator) {
		a.a.InmailCredits.SalesNavigator = fakeInt(50)
	}
	if slices.Contains(licences, LicenceRecruiter) {
		a.a.InmailCredits.Recruiter = fakeInt(100)
	}
	a.a.Usage = fakeUsage(a)
	if strings.Contains(user, "restricted") {
		at := now.Add(fakeRestrictAfter)
		a.restrictAt = &at
	}
	f.social.accounts[a.a.ID] = a
	f.seedConversationLocked(a, now)
	f.emitLocked(EventSocialAccountUpdated, external, a.a)
	return a
}

func ptr[T any](v T) *T { return &v }

func fakeLicences(user string, recruiter bool) []string {
	out := []string{LicencePremium}
	if strings.Contains(user, "salesnav") {
		out = append(out, LicenceSalesNavigator)
	}
	if recruiter {
		out = append(out, LicenceRecruiter)
	}
	return out
}

func fakeUsage(a *fakeSocialAccount) []SocialUsage {
	lim := func(p *int) int {
		if p == nil {
			return 0
		}
		return *p
	}
	used := 0
	if a.searchDay == time.Now().UTC().Format(time.DateOnly) {
		used = a.searchUsed
	}
	return []SocialUsage{
		{Action: ActionInvite, DailyLimit: lim(a.a.Limits.Invite), WeeklyLimit: a.a.WeeklyLimits.Invite},
		{Action: ActionMessage, DailyLimit: lim(a.a.Limits.Message)},
		{Action: ActionInmail, DailyLimit: lim(a.a.Limits.Inmail)},
		{Action: ActionProfileVisit, DailyLimit: lim(a.a.Limits.ProfileVisit)},
		{Action: "search", Today: used, DailyLimit: lim(a.a.Limits.Search)},
	}
}

func (f *Fake) accountLocked(externalID, id string) (*fakeSocialAccount, error) {
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	a := f.social.accounts[id]
	if a == nil || a.external != externalID {
		return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such social account")
	}
	return a, nil
}

func (f *Fake) liveAccountLocked(externalID, id string) (*fakeSocialAccount, error) {
	a, err := f.accountLocked(externalID, id)
	if err != nil {
		return nil, err
	}
	if a.a.Status == SocialRemoved {
		return nil, fakeErr(http.StatusConflict, CodeAccountUnavailable, "this account was removed")
	}
	return a, nil
}

// fakePage pages a sorted slice by an offset cursor.
func fakePage[T any](all []T, cursor string, limit int) (*Page[T], error) {
	if limit <= 0 {
		limit = 25
	}
	if limit > 100 {
		return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "limit is at most 100")
	}
	start := 0
	if cursor != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(cursor, "o"))
		if err != nil || n < 0 || !strings.HasPrefix(cursor, "o") {
			return nil, fakeErr(http.StatusBadRequest, CodeInvalidRequest, "invalid cursor")
		}
		start = n
	}
	if start > len(all) {
		start = len(all)
	}
	end := min(start+limit, len(all))
	out := &Page[T]{Data: append([]T{}, all[start:end]...)}
	if end < len(all) {
		out.Pagination = Pagination{NextCursor: "o" + strconv.Itoa(end), HasMore: true}
	}
	return out, nil
}

func (f *Fake) ListSocialAccounts(_ context.Context, externalID, status, cursor string, limit int) (*Page[SocialAccount], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.socialAccountLocked(externalID); err != nil {
		return nil, err
	}
	var all []SocialAccount
	for _, a := range f.social.accounts {
		if a.external != externalID || (status != "" && a.a.Status != status) || (status == "" && a.a.Status == SocialRemoved) {
			continue
		}
		a.a.Usage = fakeUsage(a)
		all = append(all, *cloneJSON(a.a))
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	return fakePage(all, cursor, limit)
}

func (f *Fake) GetSocialAccount(_ context.Context, externalID, id string) (*SocialAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, err := f.accountLocked(externalID, id)
	if err != nil {
		return nil, err
	}
	a.a.Usage = fakeUsage(a)
	return cloneJSON(a.a), nil
}

var fakeClock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func (f *Fake) UpdateSocialAccount(_ context.Context, externalID, id string, patch SocialAccountPatch, key string) (*SocialAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:patch:"+id, patch, func() (*SocialAccount, error) {
		a, err := f.liveAccountLocked(externalID, id)
		if err != nil {
			return nil, err
		}
		var bad []string
		over := func(field string, v *int, ceiling int) {
			if v != nil && (*v < 0 || *v > ceiling) {
				bad = append(bad, field)
			}
		}
		c := a.a.LimitCeilings
		if l := patch.Limits; l != nil {
			over("limits.invite", l.Invite, c.Invite)
			over("limits.message", l.Message, c.Message)
			over("limits.inmail", l.Inmail, c.Inmail)
			over("limits.profile_visit", l.ProfileVisit, c.ProfileVisit)
			over("limits.search", l.Search, c.Search)
		}
		if w := patch.WeeklyLimits; w != nil {
			over("weekly_limits.invite", w.Invite, c.WeeklyInvite)
		}
		if wh := patch.WorkingHours; wh != nil {
			if !fakeClock.MatchString(wh.Start) || !fakeClock.MatchString(wh.End) || wh.Start >= wh.End {
				bad = append(bad, "working_hours")
			}
			for _, d := range wh.Days {
				if d < 1 || d > 7 {
					bad = append(bad, "working_hours.days")
					break
				}
			}
		}
		if patch.Timezone != nil {
			if _, err := time.LoadLocation(*patch.Timezone); err != nil || *patch.Timezone == "" {
				bad = append(bad, "timezone")
			}
		}
		if w := patch.Warmup; w != nil {
			over("warmup.weeks", w.Weeks, 12)
			over("warmup.start_percent", w.StartPercent, 100)
		}
		over("withdraw_invites_after_days", patch.WithdrawInvitesAfterDays, 90)
		if len(bad) > 0 {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "some settings are outside what the account allows", map[string]any{"fields": bad})
		}
		set := func(dst **int, v *int) {
			if v != nil {
				*dst = fakeInt(*v)
			}
		}
		if l := patch.Limits; l != nil {
			set(&a.a.Limits.Invite, l.Invite)
			set(&a.a.Limits.Message, l.Message)
			set(&a.a.Limits.Inmail, l.Inmail)
			set(&a.a.Limits.ProfileVisit, l.ProfileVisit)
			set(&a.a.Limits.Search, l.Search)
		}
		if w := patch.WeeklyLimits; w != nil {
			set(&a.a.WeeklyLimits.Invite, w.Invite)
		}
		if patch.WorkingHours != nil {
			a.a.WorkingHours = cloneJSON(*patch.WorkingHours)
		}
		if patch.Timezone != nil {
			a.a.Timezone = *patch.Timezone
		}
		now := f.now().UTC()
		if w := patch.Warmup; w != nil {
			if !w.Enabled {
				a.a.Warmup = &SocialWarmup{Enabled: false}
			} else {
				weeks, start := 3, 20
				if w.Weeks != nil {
					weeks = *w.Weeks
				}
				if w.StartPercent != nil {
					start = *w.StartPercent
				}
				a.a.Warmup = &SocialWarmup{Enabled: true, Percent: fakeInt(start), EndsOn: ptr(now.AddDate(0, 0, 7*weeks).Format("2006-01-02"))}
			}
		}
		set(&a.a.WithdrawInvitesAfterDays, patch.WithdrawInvitesAfterDays)
		a.a.UpdatedAt = now
		a.a.Usage = fakeUsage(a)
		f.emitLocked(EventSocialAccountUpdated, externalID, a.a)
		return cloneJSON(a.a), nil
	})
}

func (f *Fake) accountVerbLocked(externalID, id, verb, key string, fn func(a *fakeSocialAccount) error) (*SocialAccount, error) {
	return replay(f, key, "social:"+verb+":"+id, nil, func() (*SocialAccount, error) {
		a, err := f.liveAccountLocked(externalID, id)
		if err != nil {
			return nil, err
		}
		if err := fn(a); err != nil {
			return nil, err
		}
		a.a.UpdatedAt = f.now().UTC()
		a.a.Usage = fakeUsage(a)
		f.emitLocked(EventSocialAccountUpdated, externalID, a.a)
		return cloneJSON(a.a), nil
	})
}

func (f *Fake) PauseSocialAccount(_ context.Context, externalID, id, key string) (*SocialAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accountVerbLocked(externalID, id, "pause", key, func(a *fakeSocialAccount) error {
		if a.a.Status == SocialActive {
			a.a.Status = SocialPaused
		}
		return nil
	})
}

func (f *Fake) ResumeSocialAccount(_ context.Context, externalID, id, key string) (*SocialAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accountVerbLocked(externalID, id, "resume", key, func(a *fakeSocialAccount) error {
		if a.a.Status == SocialPaused {
			a.a.Status = SocialActive
		}
		return nil
	})
}

func (f *Fake) ResyncSocialAccount(_ context.Context, externalID, id, key string) (*SocialAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.accountVerbLocked(externalID, id, "resync", key, func(*fakeSocialAccount) error { return nil })
}

func (f *Fake) RemoveSocialAccount(_ context.Context, externalID, id, key string) (*SocialAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:remove:"+id, nil, func() (*SocialAccount, error) {
		a, err := f.accountLocked(externalID, id)
		if err != nil {
			return nil, err
		}
		if a.a.Status != SocialRemoved {
			a.a.Status, a.a.UpdatedAt = SocialRemoved, f.now().UTC()
			for _, act := range f.social.actions {
				if act.a.AccountID == id && act.a.Status == ActionQueued {
					act.a.Status, act.a.Error = ActionFailed, &SocialError{Code: CodeAccountUnavailable, Message: "the account was removed"}
					f.emitLocked(EventSocialActionUpdated, externalID, act.a)
				}
			}
			f.emitLocked(EventSocialAccountUpdated, externalID, a.a)
		}
		return cloneJSON(a.a), nil
	})
}

// ---- time ---------------------------------------------------------------

func (f *Fake) advanceSocialLocked(now time.Time) []Event {
	var out []Event
	ids := make([]string, 0, len(f.social.sessions))
	for id := range f.social.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		s := f.social.sessions[id]
		if s.s.Ended() {
			continue
		}
		before := len(f.events)
		if !f.expireSessionLocked(s, now) && s.approveAt != nil && !now.Before(*s.approveAt) {
			s.approveAt = nil
			f.succeedLocked(s, now)
		}
		out = append(out, f.events[before:]...)
	}
	for _, a := range f.social.accounts {
		if a.restrictAt != nil && !now.Before(*a.restrictAt) && a.a.Status == SocialActive {
			reason := "LinkedIn limited this account"
			a.restrictAt, a.a.Status, a.a.StatusReason, a.a.UpdatedAt = nil, SocialRestricted, &reason, now
			out = append(out, f.emitLocked(EventSocialAccountUpdated, a.external, a.a))
		}
		if b := &a.a.Billing; b.Mode == SocialModeManaged && b.CurrentPeriodEnd != nil && a.a.Status != SocialRemoved && !now.Before(*b.CurrentPeriodEnd) {
			next := b.CurrentPeriodEnd.AddDate(0, 1, 0)
			b.CurrentPeriodEnd, a.a.UpdatedAt = &next, now
			out = append(out, f.emitLocked(EventSocialAccountUpdated, a.external, a.a))
		}
	}
	out = append(out, f.advanceOutreachLocked(now)...)
	return out
}

func fakeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = fakeKeep.ReplaceAllString(strings.ReplaceAll(s, " ", "-"), "")
	return strings.Trim(s, "-")
}
