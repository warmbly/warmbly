package seed

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type seededUniboxEmail struct {
	id           uuid.UUID
	userID       uuid.UUID
	emailID      uuid.UUID
	mailbox      uint32
	threadID     string
	messageID    string
	parentID     string
	uid          uint32
	flags        []string
	from         []string
	to           []string
	subject      string
	snippet      string
	seen         bool
	internalDate string
}

func seedUnibox(ctx context.Context, pool *pgxpool.Pool, _ *Result) error {
	if err := seedUniboxMailboxes(ctx, pool); err != nil {
		return err
	}

	rows := []seededUniboxEmail{
		{
			id: UniboxAcmeReplyID, userID: UserOwnerID, emailID: EmailAcmeAliceID,
			threadID: "seed-thread-northwind", messageID: "<seed-northwind-reply@northwind.test>",
			uid: 101, flags: []string{}, from: []string{"Aiden Park <aiden.park@northwind.test>"},
			to: []string{"Alice from Acme <alice@acme.test>"}, subject: "Re: Worth comparing inbox placement notes?",
			snippet:      "Alice, this is timely. We are seeing a few replies land in Promotions and I would like to compare notes this week.",
			seen:         false,
			internalDate: "2026-05-30T09:20:00Z",
		},
		{
			id: UniboxAcmeFollowupID, userID: UserOwnerID, emailID: EmailAcmeAliceID,
			threadID: "seed-thread-northwind", messageID: "<seed-northwind-followup@acme.test>",
			parentID: "<seed-northwind-reply@northwind.test>", uid: 102, flags: []string{"\\Seen"},
			from: []string{"Alice from Acme <alice@acme.test>"}, to: []string{"Aiden Park <aiden.park@northwind.test>"},
			subject:      "Re: Worth comparing inbox placement notes?",
			snippet:      "Thanks Aiden. I sent over two times and included the short placement report from last week.",
			seen:         true,
			internalDate: "2026-05-30T09:34:00Z",
		},
		{
			id: UniboxAcmeBounceID, userID: UserOwnerID, emailID: EmailAcmeBobID,
			threadID: "seed-thread-initech-bounce", messageID: "<seed-initech-bounce@mailer-daemon.test>",
			uid: 203, flags: []string{}, from: []string{"Mail Delivery Subsystem <mailer-daemon@initech.test>"},
			to: []string{"Bob from Acme <bob@acme.test>"}, subject: "Delivery Status Notification (Failure)",
			snippet:      "The message to beth.chen@initech.test could not be delivered. The recipient address was rejected by the server.",
			seen:         false,
			internalDate: "2026-05-29T16:05:00Z",
		},
		{
			id: UniboxAcmeOOOID, userID: UserOwnerID, emailID: EmailAcmeBobID,
			threadID: "seed-thread-pied-piper-ooo", messageID: "<seed-pied-piper-ooo@piedpiper.test>",
			uid: 204, flags: []string{"\\Seen"}, from: []string{"Carlos Diaz <carlos.diaz@pied-piper.test>"},
			to: []string{"Bob from Acme <bob@acme.test>"}, subject: "Automatic reply: Quick question",
			snippet:      "I am away until Monday with limited access to email. For anything urgent, please contact the operations alias.",
			seen:         true,
			internalDate: "2026-05-28T11:12:00Z",
		},
		{
			id: UniboxAcmeMeetingID, userID: UserOwnerID, emailID: EmailAcmeAliceID,
			threadID: "seed-thread-hooli-meeting", messageID: "<seed-hooli-meeting@hooli.test>",
			uid: 105, flags: []string{}, from: []string{"Diana Patel <diana.patel@hooli.test>"},
			to: []string{"Alice from Acme <alice@acme.test>"}, subject: "Tuesday works",
			snippet:      "Tuesday at 10 works for our team. Please send the invite and include the deliverability dashboard example.",
			seen:         false,
			internalDate: "2026-05-27T14:45:00Z",
		},
		{
			id: UniboxAcmeVendorID, userID: UserOwnerID, emailID: EmailAcmeBobID,
			threadID: "seed-thread-vandelay-vendor", messageID: "<seed-vandelay-vendor@vandelay.test>",
			uid: 205, flags: []string{"\\Seen"}, from: []string{"Greg Mori <greg.mori@vandelay.test>"},
			to: []string{"Bob from Acme <bob@acme.test>"}, subject: "Re: Vendor list cleanup",
			snippet:      "We cleaned the stale addresses and removed the shared alias from the next send. Bounce rate should be lower now.",
			seen:         true,
			internalDate: "2026-05-26T18:22:00Z",
		},
		{
			id: UniboxGlobexReplyID, userID: UserFounderID, emailID: EmailGlobexHansID,
			threadID: "seed-thread-oldfriend", messageID: "<seed-oldfriend-reply@oldfriend.test>",
			uid: 301, flags: []string{}, from: []string{"Ivan Petrov <ivan.petrov@oldfriend-co.test>"},
			to: []string{"Hans Globex <hans@globex.test>"}, subject: "Re: Warmup pool policy",
			snippet:      "The recovery pool approach makes sense. We should not put risky mailboxes back into the shared paid pool automatically.",
			seen:         false,
			internalDate: "2026-05-30T08:10:00Z",
		},
		{
			id: UniboxGlobexQuestionID, userID: UserFounderID, emailID: EmailGlobexHansID,
			threadID: "seed-thread-alphalab", messageID: "<seed-alphalab-question@alphalab.test>",
			uid: 302, flags: []string{"\\Seen"}, from: []string{"Kim Tanaka <kim.tanaka@alphalab.test>"},
			to: []string{"Hans Globex <hans@globex.test>"}, subject: "Question about sender rotation",
			snippet:      "Do you recommend adding another worker before increasing per-mailbox campaign limits?",
			seen:         true,
			internalDate: "2026-05-25T10:00:00Z",
		},
		{
			id: UniboxOwnerWelcomeID, userID: UserOwnerID, emailID: EmailOwnerSelfID,
			threadID: "seed-thread-owner-welcome", messageID: "<seed-owner-welcome@warmbly.local>",
			uid: 401, flags: []string{}, from: []string{"Warmbly <hello@warmbly.com>"},
			to: []string{"Owner Inbox <owner@warmbly.local>"}, subject: "Welcome to Warmbly",
			snippet:      "Your account is ready. Connect another mailbox or start a campaign whenever you are.",
			seen:         false,
			internalDate: "2026-05-30T07:00:00Z",
		},
		{
			id: UniboxOwnerDigestID, userID: UserOwnerID, emailID: EmailOwnerSelfID,
			threadID: "seed-thread-owner-digest", messageID: "<seed-owner-digest@warmbly.local>",
			uid: 402, flags: []string{"\\Seen"}, from: []string{"Warmbly Digest <digest@warmbly.com>"},
			to: []string{"Owner Inbox <owner@warmbly.local>"}, subject: "Your weekly deliverability digest",
			snippet:      "Inbox placement held steady across both connected mailboxes this week. No complaints, no hard bounces.",
			seen:         true,
			internalDate: "2026-05-28T07:00:00Z",
		},
	}

	for _, row := range rows {
		if err := insertUniboxEmail(ctx, pool, row); err != nil {
			return err
		}
	}
	return nil
}

func seedUniboxMailboxes(ctx context.Context, pool *pgxpool.Pool) error {
	rows := []struct {
		emailID     uuid.UUID
		uidValidity uint32
		mailbox     string
		attributes  []string
	}{
		{EmailAcmeAliceID, 1001, "INBOX", []string{"\\HasNoChildren"}},
		{EmailAcmeBobID, 2001, "INBOX", []string{"\\HasNoChildren"}},
		{EmailGlobexHansID, 3001, "INBOX", []string{"\\HasNoChildren"}},
		{EmailOwnerSelfID, 4001, "INBOX", []string{"\\HasNoChildren"}},
	}
	for _, row := range rows {
		_, err := pool.Exec(ctx, `
			INSERT INTO unibox_mailboxes (email_id, uid_validity, mailbox, attributes, highestmodseq, updated_at)
			VALUES ($1, $2, $3, $4, 1, NOW())
			ON CONFLICT (email_id, uid_validity) DO UPDATE SET
				mailbox = EXCLUDED.mailbox,
				attributes = EXCLUDED.attributes,
				highestmodseq = EXCLUDED.highestmodseq,
				updated_at = NOW()
		`, row.emailID, row.uidValidity, row.mailbox, row.attributes)
		if err != nil {
			return err
		}
	}
	return nil
}

func insertUniboxEmail(ctx context.Context, pool *pgxpool.Pool, row seededUniboxEmail) error {
	internalDate, err := time.Parse(time.RFC3339, row.internalDate)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO unibox_emails (
			id, user_id, email_id, mailbox, thread_id, message_id,
			gmail_id, parent_id, uid, mod_seq,
			flags, bcc, cc, from_addr, in_reply_to, reply_to,
			to_addr, subject, size, internal_date, sent_date,
			snippet, seen, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			'', $7, $8, 1,
			$9, '{}', '{}', $10, '{}', '{}',
			$11, $12, $13, $14, $14,
			$15, $16, NOW(), NOW()
		)
		ON CONFLICT (id) DO UPDATE SET
			email_id = EXCLUDED.email_id,
			mailbox = EXCLUDED.mailbox,
			thread_id = EXCLUDED.thread_id,
			message_id = EXCLUDED.message_id,
			parent_id = EXCLUDED.parent_id,
			uid = EXCLUDED.uid,
			flags = EXCLUDED.flags,
			from_addr = EXCLUDED.from_addr,
			to_addr = EXCLUDED.to_addr,
			subject = EXCLUDED.subject,
			size = EXCLUDED.size,
			internal_date = EXCLUDED.internal_date,
			sent_date = EXCLUDED.sent_date,
			snippet = EXCLUDED.snippet,
			seen = EXCLUDED.seen,
			updated_at = NOW()
	`, row.id, row.userID, row.emailID, row.mailbox, row.threadID, row.messageID,
		row.parentID, row.uid, row.flags, row.from, row.to, row.subject, int64(len(row.snippet)),
		internalDate, row.snippet, row.seen)
	return err
}
