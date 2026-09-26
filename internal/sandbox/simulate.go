package sandbox

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pixel and click-ticket paths as injected by internal/tasks/template.go. The
// host in the mail is whatever TRACKING_DOMAIN is set to, which need not be
// reachable from here, so the simulator keeps only the path and hits the local
// tracking service directly.
var (
	pixelRe = regexp.MustCompile(`/t/o/([0-9a-fA-F-]{36})\.png`)
	clickRe = regexp.MustCompile(`/c/([0-9a-fA-F-]{36})`)
)

type simulator struct {
	cfg  Config
	pool *pgxpool.Pool
	mp   *mailpitClient
	http *http.Client

	mu      sync.RWMutex
	hosted  map[string]hostedMailbox // lowercased address -> mailbox
	contact map[string]contactInfo   // lowercased address -> contact
}

type hostedMailbox struct {
	Email string
	Name  string
	// Seed and Host mark a placement seed inbox and who it stands in for.
	Seed bool
	Host string
}

type contactInfo struct {
	Email string
	First string
	Last  string
}

// Simulate runs the sandbox's "internet" until ctx is cancelled: it routes
// captured mail into dovecot inboxes and plays the seeded contacts (opens,
// clicks, replies). Safe to restart at any time; the mailpit Read flag is the
// cursor, so nothing is processed twice.
func Simulate(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	s := &simulator{
		cfg:  cfg,
		pool: pool,
		mp:   newMailpitClient(cfg.MailpitURL),
		http: &http.Client{
			Timeout: 15 * time.Second,
			// Click tickets 302 to their destination; the destination does not
			// resolve locally, so record the click and stop at the redirect.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		hosted:  map[string]hostedMailbox{},
		contact: map[string]contactInfo{},
	}

	if err := s.refreshDirectory(ctx); err != nil {
		return fmt.Errorf("load directory: %w", err)
	}

	fmt.Printf("simulator running: %d hosted mailboxes, %d contacts (Ctrl-C to stop)\n",
		len(s.hosted), len(s.contact))

	directoryTick := time.NewTicker(60 * time.Second)
	defer directoryTick.Stop()
	pollTick := time.NewTicker(5 * time.Second)
	defer pollTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-directoryTick.C:
			if err := s.refreshDirectory(ctx); err != nil {
				fmt.Printf("directory refresh failed: %v\n", err)
			}
		case <-pollTick.C:
			if err := s.drainMailpit(ctx); err != nil {
				fmt.Printf("mailpit poll failed: %v\n", err)
			}
		}
	}
}

// refreshDirectory reloads who exists: hosted mailboxes (all smtp_imap
// accounts, which the seeder pointed at the local stack) and seeded contacts.
func (s *simulator) refreshDirectory(ctx context.Context) error {
	hosted := map[string]hostedMailbox{}
	rows, err := s.pool.Query(ctx, `SELECT email, name, seed_scope IS NOT NULL, mail_host FROM email_accounts WHERE provider = 'smtp_imap'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m hostedMailbox
		if err := rows.Scan(&m.Email, &m.Name, &m.Seed, &m.Host); err != nil {
			rows.Close()
			return err
		}
		hosted[strings.ToLower(m.Email)] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	contacts := map[string]contactInfo{}
	rows, err = s.pool.Query(ctx, `SELECT email, first_name, last_name FROM contacts`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c contactInfo
		if err := rows.Scan(&c.Email, &c.First, &c.Last); err != nil {
			rows.Close()
			return err
		}
		contacts[strings.ToLower(c.Email)] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	s.hosted = hosted
	s.contact = contacts
	s.mu.Unlock()
	return nil
}

func (s *simulator) drainMailpit(ctx context.Context) error {
	msgs, err := s.mp.listUnread(ctx, 200)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	ids := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ids = append(ids, m.ID)
	}
	// Mark read up front so a crash mid-batch never double-delivers.
	if err := s.mp.markRead(ctx, ids); err != nil {
		return err
	}

	for _, m := range msgs {
		if err := s.handleMessage(ctx, m); err != nil {
			fmt.Printf("message %s (%q): %v\n", m.ID, m.Subject, err)
		}
	}
	return nil
}

func (s *simulator) handleMessage(ctx context.Context, summary mailpitSummary) error {
	detail, err := s.mp.message(ctx, summary.ID)
	if err != nil {
		return err
	}
	raw, err := s.mp.raw(ctx, summary.ID)
	if err != nil {
		return err
	}

	s.mu.RLock()
	hosted := s.hosted
	contacts := s.contact
	s.mu.RUnlock()

	for _, rcpt := range detail.To {
		addr := strings.ToLower(rcpt.Address)

		// Hosted recipient (another sandbox/warmup mailbox): final delivery
		// into its dovecot INBOX; the worker's real IMAP sync takes it from
		// there (warmup token verification, unibox, engagement actions).
		if m, ok := hosted[addr]; ok {
			// A seed inbox stands in for a real provider, so something has to
			// play that provider's spam filter.
			folder := "INBOX"
			if m.Seed {
				folder = seedFolder(m.Host, detail)
			}
			if err := deliverToFolder(s.cfg.IMAPAddr, addr, s.cfg.IMAPPassword, folder, raw); err != nil {
				return fmt.Errorf("deliver to %s: %w", addr, err)
			}
			fmt.Printf("delivered  %-34s %q (%s)\n", addr, detail.Subject, folder)
			continue
		}

		// Contact recipient: play the human.
		if c, ok := contacts[addr]; ok {
			sender, senderHosted := hosted[strings.ToLower(detail.From.Address)]
			go s.actAsContact(ctx, c, detail, sender, senderHosted)
		}
	}
	return nil
}

// actAsContact executes a contact persona against one received email: dwell,
// open the pixel, maybe click, maybe reply. Runs in its own goroutine.
func (s *simulator) actAsContact(ctx context.Context, c contactInfo, msg *mailpitMessage, sender hostedMailbox, senderHosted bool) {
	p := personaFor(c.Email)
	body := msg.HTML
	if body == "" {
		body = msg.Text
	}

	// A security gateway scans the message at delivery: pixel plus every
	// link, one after another, before anyone could have read it. The
	// consumer must label these as machine and never count them as clicks.
	if p.Scanned {
		s.sleep(ctx, 500*time.Millisecond, 2*time.Second)
		if task := firstMatch(pixelRe, body); task != "" {
			s.hitTracking(ctx, "/t/o/"+task+".png", c.Email)
		}
		for _, ticket := range allMatches(clickRe, body) {
			s.hitTracking(ctx, "/c/"+ticket, c.Email)
		}
		fmt.Printf("scanned    %-34s %q\n", c.Email, msg.Subject)
	}

	if p.Opens {
		if task := firstMatch(pixelRe, body); task != "" {
			// Never inside the machine window: a person needs the message
			// delivered, noticed and opened first.
			s.sleep(ctx, 15*time.Second, 40*time.Second)
			s.hitTracking(ctx, "/t/o/"+task+".png", c.Email)
			fmt.Printf("opened     %-34s %q\n", c.Email, msg.Subject)
		}
	}

	if p.Opens && p.Clicks {
		if ticket := firstMatch(clickRe, body); ticket != "" {
			s.sleep(ctx, 10*time.Second, 75*time.Second)
			s.hitTracking(ctx, "/c/"+ticket, c.Email)
			fmt.Printf("clicked    %-34s %q\n", c.Email, msg.Subject)
		}
	}

	if p.Opens && p.Replies && senderHosted {
		s.sleep(ctx, 20*time.Second, 2*time.Minute)
		text, automated := replyBody(p.Flavor, c.First)
		reply := composeReply(
			c.First+" "+c.Last, c.Email,
			sender.Name, sender.Email,
			msg.Subject, msg.MessageID, text, automated,
		)
		if err := deliverToInbox(s.cfg.IMAPAddr, strings.ToLower(sender.Email), s.cfg.IMAPPassword, reply); err != nil {
			fmt.Printf("reply from %s failed: %v\n", c.Email, err)
			return
		}
		fmt.Printf("replied    %-34s %q\n", c.Email, msg.Subject)
	}
}

func (s *simulator) hitTracking(ctx context.Context, path, actor string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.TrackingURL+path, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	resp, err := s.http.Do(req)
	if err != nil {
		fmt.Printf("tracking hit %s for %s failed: %v\n", path, actor, err)
		return
	}
	resp.Body.Close()
}

// sleep waits a humanized random duration, returning early on shutdown.
func (s *simulator) sleep(ctx context.Context, min, max time.Duration) {
	d := min + time.Duration(rand.Int63n(int64(max-min)))
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func firstMatch(re *regexp.Regexp, body string) string {
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// allMatches returns every distinct first capture group, in document order.
func allMatches(re *regexp.Regexp, body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if len(m) < 2 || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		out = append(out, m[1])
	}
	return out
}
