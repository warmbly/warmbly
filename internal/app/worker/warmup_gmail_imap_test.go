package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/worker/wmail"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// gmailSession stands in for imap.gmail.com: folders by special-use, and where the message currently sits.
type gmailSession struct {
	folders []models.Mailbox
	held    map[string]uint32
	findErr error
	calls   []string
}

func gmailFolders(except ...string) []models.Mailbox {
	all := []models.Mailbox{
		{Name: "INBOX"},
		{Name: "[Gmail]/Spam", Attrs: []string{`\Junk`}},
		{Name: "[Gmail]/All Mail", Attrs: []string{`\All`}},
		{Name: "[Gmail]/Important", Attrs: []string{`\Important`}},
		{Name: "Warmbly"},
	}
	return slices.DeleteFunc(all, func(b models.Mailbox) bool { return slices.Contains(except, b.Name) })
}

func (s *gmailSession) AllFolders(context.Context) ([]models.Mailbox, error) { return s.folders, nil }

func (s *gmailSession) FindUIDInFolder(_ context.Context, box, _ string) (uint32, error) {
	if s.findErr != nil {
		return 0, s.findErr
	}
	return s.held[box], nil
}

func (s *gmailSession) MoveToFolder(_ context.Context, src, dst string, uid uint32) (bool, error) {
	s.calls = append(s.calls, fmt.Sprintf("move %s -> %s", src, dst))
	delete(s.held, src)
	s.held[dst] = uid + 100
	return true, nil
}

func (s *gmailSession) CopyToFolder(_ context.Context, src, dst string, _ uint32) error {
	s.calls = append(s.calls, fmt.Sprintf("copy %s -> %s", src, dst))
	return nil
}

func (s *gmailSession) MarkAsRead(_ context.Context, box string, _ uint32) error {
	s.calls = append(s.calls, "seen "+box)
	return nil
}

func (s *gmailSession) MarkImportant(_ context.Context, box string, _ uint32) error {
	s.calls = append(s.calls, "flagged "+box)
	return nil
}

func (s *gmailSession) Close() error { return nil }

type gmailDialer struct {
	session *gmailSession
	err     error
	dials   int
}

func (d *gmailDialer) mail() *wmail.WMail {
	return &wmail.WMail{EmailType: models.InboxProviderGoogle, GoogleData: &wmail.GoogleData{
		Client: &goog.Client{},
		IMAP: &wmail.GmailIMAP{Dial: func(context.Context) (wmail.GmailIMAPSession, error) {
			d.dials++
			if d.err != nil {
				return nil, d.err
			}
			return d.session, nil
		}},
	}}
}

func gmailAction(placement string, actions ...string) models.WarmupEmailAction {
	return models.WarmupEmailAction{EmailID: uuid.New(), GmailID: "gm-1", RFCMessageID: "<warm@test>",
		Placement: placement, TargetFolder: "Warmbly", Actions: actions}
}

func TestGmailRescueOverIMAPFollowsThePlacement(t *testing.T) {
	for placement, dst := range map[string]string{
		models.WarmupPlacementInbox:   "INBOX",
		models.WarmupPlacementFolder:  "Warmbly",
		models.WarmupPlacementArchive: "[Gmail]/All Mail",
	} {
		s := &gmailSession{folders: gmailFolders(), held: map[string]uint32{"[Gmail]/Spam": 7}}
		d := &gmailDialer{session: s}
		outcome := ""
		settled := (&WorkerService{}).runGmailIMAPEngagement(context.Background(), d.mail(), gmailAction(placement, models.WarmupActionRescueFromSpam), &outcome)
		if !settled[models.WarmupActionRescueFromSpam] || outcome != models.WarmupRescueMoved {
			t.Fatalf("%s: settled=%v outcome=%q", placement, settled, outcome)
		}
		if want := "move [Gmail]/Spam -> " + dst; len(s.calls) != 1 || s.calls[0] != want {
			t.Fatalf("%s: calls %v, want %q", placement, s.calls, want)
		}
	}
}

func TestGmailRescueOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		held    map[string]uint32
		findErr error
		want    string
	}{
		{"already out of spam", map[string]uint32{"[Gmail]/All Mail": 3}, nil, models.WarmupRescueAlreadyOut},
		{"nowhere", map[string]uint32{}, nil, models.WarmupRescueGone},
		{"search failed on the network, left to the API", map[string]uint32{}, errx.ErrMailServerUnreachable, ""},
	} {
		s := &gmailSession{folders: gmailFolders(), held: tc.held, findErr: tc.findErr}
		outcome := ""
		settled := (&WorkerService{}).runGmailIMAPEngagement(context.Background(), (&gmailDialer{session: s}).mail(),
			gmailAction(models.WarmupPlacementFolder, models.WarmupActionRescueFromSpam), &outcome)
		if settled[models.WarmupActionRescueFromSpam] != (tc.want != "") || outcome != tc.want || len(s.calls) != 0 {
			t.Fatalf("%s: settled=%v outcome=%q calls=%v", tc.name, settled, outcome, s.calls)
		}
	}
}

func TestGmailEngagementSetsSeenStarAndImportantOverIMAP(t *testing.T) {
	s := &gmailSession{folders: gmailFolders(), held: map[string]uint32{"[Gmail]/All Mail": 9, "Warmbly": 2}}
	acts := []string{models.WarmupActionMarkRead, models.WarmupActionStar, models.WarmupActionMarkImportant}
	settled := (&WorkerService{}).runGmailIMAPEngagement(context.Background(), (&gmailDialer{session: s}).mail(),
		gmailAction(models.WarmupPlacementFolder, acts...), nil)
	for _, act := range acts {
		if !settled[act] {
			t.Fatalf("%s left to the API: %v", act, settled)
		}
	}
	want := []string{"seen [Gmail]/All Mail", "flagged [Gmail]/All Mail", "copy [Gmail]/All Mail -> [Gmail]/Important"}
	if !slices.Equal(s.calls, want) {
		t.Fatalf("calls %v, want %v", s.calls, want)
	}
}

// A view hidden from IMAP costs that one action its IMAP path, not the mailbox.
func TestGmailHiddenFolderHandsOnlyThatActionToTheAPI(t *testing.T) {
	s := &gmailSession{folders: gmailFolders("[Gmail]/Important"), held: map[string]uint32{"[Gmail]/All Mail": 9}}
	d := &gmailDialer{session: s}
	mail := d.mail()
	settled := (&WorkerService{}).runGmailIMAPEngagement(context.Background(), mail,
		gmailAction(models.WarmupPlacementFolder, models.WarmupActionMarkRead, models.WarmupActionMarkImportant), nil)
	if !settled[models.WarmupActionMarkRead] || settled[models.WarmupActionMarkImportant] || mail.GoogleData.IMAP.Refused() != "" {
		t.Fatalf("settled=%v refused=%q", settled, mail.GoogleData.IMAP.Refused())
	}
}

func TestGmailIMAPRefusalIsRememberedForTheMailbox(t *testing.T) {
	for _, refusal := range []error{wmail.ErrGmailIMAPScope, errx.ErrMailAuthenticationFailed, errx.ErrMailAuthorizationFailed} {
		d := &gmailDialer{err: refusal}
		mail := d.mail()
		w := &WorkerService{}
		for range 3 {
			outcome := ""
			if settled := w.runGmailIMAPEngagement(context.Background(), mail, gmailAction(models.WarmupPlacementInbox, models.WarmupActionRescueFromSpam, models.WarmupActionMarkRead), &outcome); len(settled) != 0 || outcome != "" {
				t.Fatalf("%v: a refused mailbox settled %v (outcome %q) instead of handing it to the API", refusal, settled, outcome)
			}
		}
		if d.dials != 1 || mail.GoogleData.IMAP.Refused() == "" {
			t.Fatalf("%v: dials=%d refused=%q", refusal, d.dials, mail.GoogleData.IMAP.Refused())
		}
	}
}

// A network failure leaves the action to the Gmail API for this batch and is not remembered as a refusal.
func TestGmailIMAPNetworkFailureIsNotAFallback(t *testing.T) {
	d := &gmailDialer{err: errx.ErrMailServerUnreachable}
	mail := d.mail()
	w := &WorkerService{}
	for range 2 {
		outcome := ""
		settled := w.runGmailIMAPEngagement(context.Background(), mail, gmailAction(models.WarmupPlacementInbox, models.WarmupActionRescueFromSpam), &outcome)
		if settled[models.WarmupActionRescueFromSpam] || outcome != "" {
			t.Fatalf("settled=%v outcome=%q", settled, outcome)
		}
	}
	if d.dials != 2 || mail.GoogleData.IMAP.Refused() != "" {
		t.Fatalf("a network failure was remembered: dials=%d refused=%q", d.dials, mail.GoogleData.IMAP.Refused())
	}
}

func TestGmailIMAPSessionIsReusedAcrossBatches(t *testing.T) {
	s := &gmailSession{folders: gmailFolders(), held: map[string]uint32{"[Gmail]/All Mail": 9}}
	d := &gmailDialer{session: s}
	mail := d.mail()
	w := &WorkerService{}
	for range 3 {
		w.runGmailIMAPEngagement(context.Background(), mail, gmailAction(models.WarmupPlacementFolder, models.WarmupActionMarkRead), nil)
	}
	if d.dials != 1 {
		t.Fatalf("dialed %d sessions for back-to-back batches", d.dials)
	}
}

// Without a Message-ID or an IMAP holder the leg stays entirely on the API.
func TestGmailIMAPNeedsAMessageID(t *testing.T) {
	d := &gmailDialer{session: &gmailSession{folders: gmailFolders(), held: map[string]uint32{}}}
	action := gmailAction(models.WarmupPlacementFolder, models.WarmupActionMarkRead)
	action.RFCMessageID = ""
	if settled := (&WorkerService{}).runGmailIMAPEngagement(context.Background(), d.mail(), action, nil); settled != nil || d.dials != 0 {
		t.Fatalf("settled=%v dials=%d", settled, d.dials)
	}
}

func TestGmailWarmupLegRunsOnlyTheUnsettledActionsOnTheAPI(t *testing.T) {
	var modified []string
	transport := filingTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/modify") {
			body, _ := io.ReadAll(r.Body)
			modified = append(modified, string(body))
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header), Request: r}, nil
	})
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: transport})
	d := &gmailDialer{session: &gmailSession{folders: gmailFolders("[Gmail]/Important"), held: map[string]uint32{"[Gmail]/All Mail": 9}}}
	mail := d.mail()
	if err := mail.GoogleData.Client.Init(ctx, &oauth2.Token{AccessToken: "t", Expiry: time.Now().Add(time.Hour)}, oauth2.Config{}); err != nil {
		t.Fatal(err)
	}
	w := newLoadingWorker(&capturedEvents{})
	action := gmailAction(models.WarmupPlacementFolder, models.WarmupActionMarkRead, models.WarmupActionStar, models.WarmupActionMarkImportant)
	w.mailManager.Emails[action.EmailID] = mail
	if err := w.HandleWarmupAction(ctx, action); err != nil {
		t.Fatal(err)
	}
	if len(modified) != 1 || !strings.Contains(modified[0], "IMPORTANT") {
		t.Fatalf("the API ran %v, want only the Important label IMAP could not reach", modified)
	}
}
