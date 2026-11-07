package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveUniboxSearchParticipants(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	id := f.message(t, repo, models.FolderInbox)
	if _, err := handle.Exec(t.Context(), `UPDATE unibox_emails SET
		from_addr = ARRAY['Élodie Needle <elodie@needle.test>', 'Redwood', 'Birch'],
		to_addr = ARRAY['Recipient <recipient@delivery.test>'],
		cc = ARRAY['Cedar Person <cedar@forest.test>', E'rate%_\\special'],
		body_text = 'A confidential pineapple proposal with exact wording.' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"elodie@needle.test", 1}, {"needle.te", 1}, {"Needle", 1}, {"ÉLODIE", 1},
		{"RECIPIENT@DELIVERY.TEST", 1}, {"cedar@forest", 1}, {"Cedar Person", 1},
		{`rate%_\special`, 1}, {"pineapple", 1}, {"pine", 1}, {`"exact wording"`, 1},
		{"pineapple -proposal", 0}, {"absent OR pineapple", 1},
		{"fi", 1}, {"ab", 0}, {"a.b", 0}, {"%", 0}, {"_", 0}, {`\`, 0},
		{"Redwood\nBirch", 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			res, err := repo.Search(t.Context(), f.org, &models.MailSearchParams{Subject: &tc.query, PageSize: 50})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Data) != tc.want || (tc.want == 1 && res.Data[0].ID != id) {
				t.Fatalf("Search(%q) = %+v, want %d matching message", tc.query, res.Data, tc.want)
			}
		})
	}
}

func TestLiveUniboxSearchMatchingThreadAggregation(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	other := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	base := time.Now().UTC().Add(-time.Hour)
	var matching []uuid.UUID
	for i := range 4 {
		id := f.message(t, repo, models.FolderInbox)
		body := "pineapple"
		if i == 2 {
			body = "unrelated"
		}
		thread := "search-aggregation"
		if i == 3 {
			thread = ""
		}
		if _, err := handle.Exec(t.Context(), `UPDATE unibox_emails SET thread_id=$2, body_text=$3,
			internal_date=$4, seen=$5 WHERE id=$1`, id, thread, body, base.Add(time.Duration(i)*time.Minute), i != 0); err != nil {
			t.Fatal(err)
		}
		matching = append(matching, id)
	}
	foreign := other.message(t, repo, models.FolderInbox)
	if _, err := handle.Exec(t.Context(), `UPDATE unibox_emails SET thread_id='search-aggregation', body_text='pineapple', seen=false WHERE id=$1`, foreign); err != nil {
		t.Fatal(err)
	}
	q := "pineapple"
	params := &models.MailSearchParams{Subject: &q, PageSize: 1}
	first, err := repo.Search(t.Context(), f.org, params)
	if err != nil || first == nil || len(first.Data) != 1 || first.Data[0].ID != matching[3] || first.Data[0].MessageCount != 1 || first.Data[0].HasUnread || !first.Pagination.HasMore {
		t.Fatalf("singleton page=%+v error=%v", first, err)
	}
	params.Cursor = *first.Pagination.NextCursor
	second, err := repo.Search(t.Context(), f.org, params)
	if err != nil || second == nil || len(second.Data) != 1 || second.Data[0].ID != matching[1] || second.Data[0].MessageCount != 2 || !second.Data[0].HasUnread || second.Pagination.HasMore {
		t.Fatalf("matched conversation page=%+v error=%v", second, err)
	}
	params.Cursor = ""
	unseen := true
	params.Unseen = &unseen
	unread, err := repo.Search(t.Context(), f.org, params)
	if err != nil || unread == nil || len(unread.Data) != 1 || unread.Data[0].ID != matching[0] || unread.Data[0].MessageCount != 1 {
		t.Fatalf("unread matching messages=%+v error=%v", unread, err)
	}
}

func TestLiveUniboxReadStreamErrors(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	f.message(t, NewUniboxRepository(handle), models.FolderInbox)
	config, err := pgxpool.ParseConfig(os.Getenv("WARMBLY_TEST_DB"))
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns, config.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := &uniboxRepository{db: &db.DB{Pool: pool}}
	for _, tc := range []struct {
		name, table string
		read        func(context.Context) error
	}{
		{"threads", "unibox_emails", func(ctx context.Context) error {
			res, err := repo.Search(ctx, f.org, &models.MailSearchParams{PageSize: 50})
			if err != nil && res != nil {
				t.Error("failed search returned a partial page")
			}
			return err
		}},
		{"previews", "unibox_emails", func(ctx context.Context) error {
			_, err := repo.GetIncoming(ctx, f.user, 50, "")
			return err
		}},
		{"labels", "unibox_thread_labels", func(ctx context.Context) error {
			_, err := repo.ListThreadLabels(ctx, f.org, "search-errors")
			return err
		}},
		{"snoozes", "unibox_snoozes", func(ctx context.Context) error {
			_, err := repo.ListSnoozes(ctx, f.org, f.user)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.read(t.Context()); err != nil {
				t.Fatal("prepare read", err)
			}
			tx, err := handle.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			if _, err := tx.Exec(t.Context(), "LOCK TABLE "+tc.table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(t.Context(), "SET statement_timeout = '20ms'"); err != nil {
				t.Fatal(err)
			}
			err = tc.read(t.Context())
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
				t.Fatalf("interrupted read error=%v, want query cancellation, not empty success", err)
			}
			if _, err := pool.Exec(t.Context(), "SET statement_timeout = 0"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLiveUniboxSearchFiltersAndTies(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	now := time.Now().UTC().Truncate(time.Second)
	ours := f.mailboxAddress(t, repo)
	second := *f
	second.mailbox = visibilityMailbox(t, handle.Pool, f, now)
	thread := "filtered-search"
	old := f.scopedMessage(t, repo, thread, "Alex <"+ours+">", models.FolderSent, now.Add(-2*time.Minute))
	received := f.scopedMessage(t, repo, thread, "them@example.com", models.FolderInbox, now.Add(-time.Minute))
	latest := second.scopedMessage(t, repo, thread, "Alex <"+ours+">", models.FolderSent, now)
	category := visibilityCategory(t, NewGroupRepostory(handle, models.Categories), f, "Search label")
	if _, err := repo.SetThreadLabels(t.Context(), f.org, f.user, thread, []uuid.UUID{category}); err != nil {
		t.Fatal(err)
	}
	q, sent, incoming, direction := "scope", models.FolderSent, models.FolderInbox, "received"
	yes := true
	boundary := now.Add(-90 * time.Second)
	for _, tc := range []struct {
		name   string
		params models.MailSearchParams
		id     uuid.UUID
		count  int64
	}{
		{"all mailboxes", models.MailSearchParams{}, latest, 3},
		{"mailbox", models.MailSearchParams{EmailAccountIDs: []uuid.UUID{f.mailbox}}, received, 2},
		{"second mailbox", models.MailSearchParams{EmailAccountIDs: []uuid.UUID{second.mailbox}}, latest, 1},
		{"since", models.MailSearchParams{Since: &boundary}, latest, 2},
		{"before", models.MailSearchParams{Before: &boundary}, old, 1},
		{"sent", models.MailSearchParams{Folder: &sent}, latest, 2},
		{"inbox", models.MailSearchParams{Folder: &incoming}, received, 1},
		{"sender", models.MailSearchParams{Sender: &ours}, latest, 2},
		{"direction", models.MailSearchParams{Direction: &direction}, received, 1},
		{"awaiting reply", models.MailSearchParams{AwaitingReply: &yes}, latest, 3},
		{"not awaiting in mailbox", models.MailSearchParams{AwaitingReply: &yes, EmailAccountIDs: []uuid.UUID{f.mailbox}}, uuid.Nil, 0},
		{"category", models.MailSearchParams{CategoryIDs: []uuid.UUID{category}}, latest, 3},
		{"unknown category", models.MailSearchParams{CategoryIDs: []uuid.UUID{uuid.New()}}, uuid.Nil, 0},
		{"uncategorized", models.MailSearchParams{Uncategorized: &yes}, uuid.Nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.params.Subject, tc.params.PageSize = &q, 50
			res, err := repo.Search(t.Context(), f.org, &tc.params)
			if err != nil {
				t.Fatal(err)
			}
			if tc.id == uuid.Nil {
				if len(res.Data) != 0 {
					t.Fatalf("expected no matching conversation: %+v", res.Data)
				}
				return
			}
			if len(res.Data) != 1 || res.Data[0].ID != tc.id || res.Data[0].MessageCount != tc.count || !res.Data[0].HasUnread || len(res.Data[0].Labels) != 1 || res.Data[0].Labels[0].ID != category {
				t.Fatalf("unexpected representative or thread state: %+v", res.Data)
			}
		})
	}
	if _, err := repo.UpsertSnoozes(t.Context(), f.org, f.user, []string{thread}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, snoozed := range []*bool{nil, &yes} {
		res, err := repo.Search(t.Context(), f.org, &models.MailSearchParams{Subject: &q, Snoozed: snoozed, PageSize: 50})
		if err != nil || (snoozed == nil && len(res.Data) != 0) || (snoozed != nil && (len(res.Data) != 1 || res.Data[0].ID != latest)) {
			t.Fatalf("snoozed=%v result=%+v error=%v", snoozed, res, err)
		}
	}
	first := f.scopedMessage(t, repo, "search-tie", "them@example.com", models.FolderInbox, now)
	last := f.scopedMessage(t, repo, "search-tie", "them@example.com", models.FolderInbox, now)
	if first.String() > last.String() {
		last = first
	}
	res, err := repo.Search(t.Context(), f.org, &models.MailSearchParams{Subject: &q, PageSize: 50})
	if err != nil || len(res.Data) != 1 || res.Data[0].ID != last || res.Data[0].MessageCount != 2 {
		t.Fatalf("date tie result=%+v error=%v", res, err)
	}
}

func TestLiveUniboxSearchBroadQueryPlan(t *testing.T) {
	handle := liveUniboxFolderDB(t)
	f := newUniboxFolderFixture(t, handle.Pool)
	mailboxes := []uuid.UUID{f.mailbox}
	for range 7 {
		mailboxes = append(mailboxes, visibilityMailbox(t, handle.Pool, f, time.Now()))
	}
	rare := newUniboxFolderFixture(t, handle.Pool)
	repo := NewUniboxRepository(handle)
	rareID := rare.message(t, repo, models.FolderInbox)
	if _, err := handle.Exec(t.Context(), `UPDATE unibox_emails SET
		from_addr=ARRAY['rareleaf@isolated.test'], body_text='Rarebodypineapple' WHERE id=$1`, rareID); err != nil {
		t.Fatal(err)
	}
	rows := 20000
	if value := os.Getenv("WARMBLY_UNIBOX_SEARCH_ROWS"); value != "" {
		var err error
		rows, err = strconv.Atoi(value)
		if err != nil || rows < 1000 {
			t.Fatal("WARMBLY_UNIBOX_SEARCH_ROWS must be at least 1000")
		}
	}
	for start := 1; start <= rows; start += 10000 {
		if _, err := handle.Exec(t.Context(), `INSERT INTO unibox_emails
		(id,user_id,email_id,thread_id,message_id,from_addr,to_addr,subject,snippet,body_text,internal_date,seen,folder)
		SELECT gen_random_uuid(),$1,($2::uuid[])[1 + ((g-1) % cardinality($2::uuid[]))],
		'broad-' || (g/4)::text,g::text || '@search.test',
		ARRAY['Sender <sender@search.test>'],ARRAY['Recipient <recipient@search.test>'],
		'Reply about a project','A project update',repeat('A detailed review of the project and next steps. ',40),
		NOW() - g * INTERVAL '1 second',g%2=0,'inbox'
		FROM generate_series($3::int,$4::int) g`, f.user, mailboxes, start, min(start+9999, rows)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := handle.Exec(t.Context(), "ANALYZE unibox_emails"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		org   uuid.UUID
		want  int
	}{
		{"re", f.org, 50}, {"reply", f.org, 50}, {"det", f.org, 50}, {"detailed", f.org, 50},
		{"sender@search.test", f.org, 50}, {"absentneedle@nonexistent.test", f.org, 0},
		{"rareleaf@isolated.test", rare.org, 1}, {"rarebodypi", rare.org, 1},
	} {
		t.Run(tc.query, func(t *testing.T) {
			query, args := uniboxSearchQuery(tc.org, &models.MailSearchParams{Subject: &tc.query, PageSize: 50})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			start := time.Now()
			planRows, err := handle.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+query, args...)
			if err != nil {
				t.Fatal(err)
			}
			defer planRows.Close()
			var plan strings.Builder
			for planRows.Next() {
				var line string
				if err := planRows.Scan(&line); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintln(&plan, line)
			}
			if err := planRows.Err(); err != nil {
				planned, planErr := handle.Query(t.Context(), "EXPLAIN "+query, args...)
				if planErr == nil {
					defer planned.Close()
					for planned.Next() {
						var line string
						if planned.Scan(&line) == nil {
							t.Log(line)
						}
					}
				}
				t.Fatal(err)
			}
			t.Logf("%d messages, query %q: %s\n%s", rows, tc.query, time.Since(start), plan.String())
			if strings.Contains(plan.String(), "WindowAgg") || !strings.Contains(plan.String(), "CTE page") {
				t.Fatal("search must page before aggregating thread state")
			}
			if tc.query == "absentneedle@nonexistent.test" && !strings.Contains(plan.String(), "idx_unibox_emails_participants_trgm") {
				t.Fatal("participant search did not use its trigram index")
			}
			searchCtx, cancelSearch := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancelSearch()
			res, err := repo.Search(searchCtx, tc.org, &models.MailSearchParams{Subject: &tc.query, PageSize: 50})
			if err != nil || res == nil || len(res.Data) != tc.want || (tc.want == 1 && res.Data[0].ID != rareID) {
				t.Fatalf("unexpected search result=%+v error=%v", res, err)
			}
		})
	}
}
