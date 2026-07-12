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
// domain in the mail is the (unresolvable) TRACKING_DOMAIN, so the simulator
// keeps only the path and hits the local tracking service directly.
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
	rows, err := s.pool.Query(ctx, `SELECT email, name FROM email_accounts WHERE provider = 'smtp_imap'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m hostedMailbox
		if err := rows.Scan(&m.Email, &m.Name); err != nil {
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
		if _, ok := hosted[addr]; ok {
			if err := deliverToInbox(s.cfg.IMAPAddr, addr, s.cfg.IMAPPassword, raw); err != nil {
				return fmt.Errorf("deliver to %s: %w", addr, err)
			}
			fmt.Printf("delivered  %-34s %q\n", addr, detail.Subject)
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

	if p.Opens {
		if task := firstMatch(pixelRe, body); task != "" {
			s.sleep(ctx, 15*time.Second, 90*time.Second)
			s.hitTracking(ctx, "/t/o/"+task+".png", c.Email)
			fmt.Printf("opened     %-34s %q\n", c.Email, msg.Subject)
		}
	}

	if p.Opens && p.Clicks {
		if ticket := firstMatch(clickRe, body); ticket != "" {
			s.sleep(ctx, 20*time.Second, 3*time.Minute)
			s.hitTracking(ctx, "/c/"+ticket, c.Email)
			fmt.Printf("clicked    %-34s %q\n", c.Email, msg.Subject)
		}
	}

	if p.Opens && p.Replies && senderHosted {
		s.sleep(ctx, 45*time.Second, 5*time.Minute)
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
