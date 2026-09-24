package worker

import (
	"context"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/worker/mailmanager"
	"github.com/warmbly/warmbly/internal/models"
)

type capturedEvents struct {
	mu     sync.Mutex
	events []models.JobEventType
	bodies []any
}

func (c *capturedEvents) on(t models.JobEventType, _ string, body any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events, c.bodies = append(c.events, t), append(c.bodies, body)
	return nil
}

func newLoadingWorker(c *capturedEvents) *WorkerService {
	return &WorkerService{mailManager: mailmanager.NewMailManager(c.on, nil, nil, nil, nil, nil, nil)}
}

// A mail server that accepts and never speaks holds a load for minutes; it must not hold the command queue.
func TestAddEmailDoesNotBlockOnASilentServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepted atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			defer conn.Close()
		}
	}()
	port, _ := strconv.Atoi(strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))

	w := newLoadingWorker(&capturedEvents{})
	id := uuid.New()
	e := &models.AddWorkerEmail{
		ID: id, UserID: uuid.New(), Type: models.InboxProviderSMTPIMAP, ImapSync: true, Email: "a@example.test",
		SmtpImap: &models.AddWorkerEmailSmtpImapData{Credentials: &models.SmtpImap{
			IMAP: &models.Service{Host: "127.0.0.1", Port: port, Username: "a", Password: "b", Security: models.MailSecurityTLS},
			SMTP: &models.Service{Host: "127.0.0.1", Port: port, Username: "a", Password: "b", Security: models.MailSecurityTLS},
		}},
	}
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := w.HandleAddEmail(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("three ADD_EMAILs held the queue for %v", took)
	}
	time.Sleep(200 * time.Millisecond)
	if n := accepted.Load(); n != 1 {
		t.Fatalf("a republished ADD_EMAIL dialed again: %d connections", n)
	}

	// A command for the mailbox waits for the load, within its own deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := w.loadedMailbox(ctx, id); ok {
		t.Fatal("a mailbox still dialing was handed out")
	}
	if err := w.HandleRemoveEmail(context.Background(), &models.RemoveWorkerEmail{EmailID: id.String()}); err != nil {
		t.Fatal(err)
	}
	if v, ok := w.loads.Load(id); !ok || !v.(*mailboxLoad).removed.Load() {
		t.Fatal("a removal during the load was not recorded, so the mailbox would load after it was deleted")
	}
}

func TestAddEmailReportsAMailboxThatCannotLoad(t *testing.T) {
	c := &capturedEvents{}
	w := newLoadingWorker(c)
	id := uuid.New()
	if err := w.HandleAddEmail(context.Background(), &models.AddWorkerEmail{ID: id, UserID: uuid.New(), Type: models.InboxProviderSMTPIMAP}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, ok := w.loadedMailbox(ctx, id); ok {
		t.Fatal("a mailbox without credentials loaded")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) != 1 || c.events[0] != models.JobEventTypeEmailAuthError {
		t.Fatalf("events = %v, want one auth error so the mailbox shows it and stops", c.events)
	}
	if ev, ok := c.bodies[0].(models.EmailErrorEvent); !ok || ev.EmailAccountID != id.String() {
		t.Fatalf("event body = %+v", c.bodies[0])
	}
}

func TestAddEmailLoadsAndCommandsFindIt(t *testing.T) {
	w := newLoadingWorker(&capturedEvents{})
	id := uuid.New()
	e := &models.AddWorkerEmail{
		ID: id, UserID: uuid.New(), Type: models.InboxProviderSMTPIMAP, Email: "a@example.test",
		SmtpImap: &models.AddWorkerEmailSmtpImapData{Credentials: &models.SmtpImap{
			IMAP: &models.Service{Host: "imap.example.test", Port: 993}, SMTP: &models.Service{Host: "smtp.example.test", Port: 587},
		}},
	}
	if err := w.HandleAddEmail(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, ok := w.loadedMailbox(ctx, id); !ok {
		t.Fatal("a command right behind the ADD_EMAIL did not find the mailbox")
	}
}

// ADD, REMOVE, ADD while the first load is still dialing ends loaded, as it did when commands ran in order.
func TestAddEmailAfterARemoveDuringTheLoadIsKept(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	conns := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns <- c
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	w := newLoadingWorker(&capturedEvents{})
	id := uuid.New()
	slow := &models.AddWorkerEmail{
		ID: id, UserID: uuid.New(), Type: models.InboxProviderSMTPIMAP, ImapSync: true, Email: "a@example.test",
		SmtpImap: &models.AddWorkerEmailSmtpImapData{Credentials: &models.SmtpImap{
			IMAP: &models.Service{Host: "127.0.0.1", Port: port, Username: "a", Password: "b", Security: models.MailSecurityTLS},
			SMTP: &models.Service{Host: "127.0.0.1", Port: port, Username: "a", Password: "b", Security: models.MailSecurityTLS},
		}},
	}
	readd := *slow
	readd.ImapSync = false

	_ = w.HandleAddEmail(context.Background(), slow)
	c := <-conns
	_ = w.HandleRemoveEmail(context.Background(), &models.RemoveWorkerEmail{EmailID: id.String()})
	_ = w.HandleAddEmail(context.Background(), &readd)
	_ = c.Close() // the first load gives up; the re-add runs after it

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		if _, ok := w.loadedMailbox(ctx, id); ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the add that followed a removal was dropped")
}
