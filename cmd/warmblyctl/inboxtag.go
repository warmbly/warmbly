package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/inboxtag"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/repository"
)

// Automatic tagging classifies mail as it arrives, which is no use on the day
// you turn it on: the inbox you want sorted is the one already sitting there.
// This runs the same classifier over history.
//
// Deliberately an operator command rather than a button. A backfill over a
// large mailbox is real money and real time, so it is bounded, resumable,
// interruptible, and it can be costed before it is run.

func runInboxTag(ctx context.Context, args []string) error {
	if len(args) == 0 {
		inboxTagUsage(os.Stderr)
		return errors.New("`inbox-tag` needs a subcommand. Pick one from the list above.")
	}
	switch args[0] {
	case "help", "-h", "--help":
		inboxTagUsage(os.Stdout)
		return nil
	case "backfill":
		return runInboxTagBackfill(ctx, args[1:])
	case "follow-ups":
		return runInboxTagFollowUps(ctx, args[1:])
	}
	inboxTagUsage(os.Stderr)
	return fmt.Errorf("unknown subcommand `inbox-tag %s`. Pick one from the list above.", args[0])
}

func inboxTagUsage(w *os.File) {
	fmt.Fprint(w, "Classify mail that arrived before automatic tagging was switched on.\n\nUsage:\n  warmblyctl inbox-tag <subcommand> [flags]\n\nSubcommands:\n")
	for _, c := range commands {
		if !strings.HasPrefix(c.name, "inbox-tag ") {
			continue
		}
		fmt.Fprintf(w, "  %-28s %s\n    %s\n", strings.TrimPrefix(c.name, "inbox-tag "), c.summary, c.example)
	}
}

func runInboxTagBackfill(ctx context.Context, args []string) error {
	fs := newFlagSet("inbox-tag backfill")
	org := fs.String("org", "", "organization id, or the owner's email address")
	days := fs.Int("days", 30, "how far back to go")
	limit := fs.Int("limit", 200, "most messages to classify in this run")
	dryRun := fs.Bool("dry-run", false, "list what would be classified and call nothing")
	recheck := fs.Bool("recheck-cold-inbound", false, "re-classify mail stored as cold_inbound in threads a campaign send belongs to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if strings.TrimSpace(*org) == "" {
		return errors.New("--org is required: a backfill is scoped to one workspace")
	}

	// Refuse early and plainly. Discovering the feature is off after waiting
	// for a long run to produce nothing is a worse way to learn it.
	if !config.InboxTaggingEnabled() && !*dryRun {
		return errors.New("automatic tagging is off on this instance. Set TYPESAFE_API_KEY and INBOX_TAGGING_ENABLED=true, or pass --dry-run to see what a run would cover.")
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	orgID, err := resolveOrgID(ctx, c, *org)
	if err != nil {
		return err
	}

	repo := repository.NewInboxTagRepository(c.db.Pool)
	categories := repository.NewTagCategoryStore(c.db.Pool)

	// The asker is nil-safe on a dry run, but constructing it is what proves a
	// key is present, so it is built either way.
	svc := inboxtag.NewService(
		inboxtag.NewClient(config.TypeSafeAPIKey()),
		repo,
		categories,
		categories,
		true,
	)
	svc.WireSettings(repository.NewAdvancedOutreachRepository(c.db.Pool))

	since := time.Now().AddDate(0, 0, -*days)

	if *dryRun {
		fmt.Printf("Dry run: nothing will be classified and nothing will be written.\n\n")
	}
	fmt.Printf("Workspace %s, mail since %s, at most %d messages.\n",
		orgID, since.Format("2 Jan 2006"), *limit)

	start := time.Now()
	p, err := svc.Backfill(ctx, orgID, inboxtag.BackfillOptions{
		Since:              since,
		Limit:              *limit,
		DryRun:             *dryRun,
		RecheckColdInbound: *recheck,
		OnProgress: func(p inboxtag.BackfillProgress, subject string) {
			// One line per message. A long run that goes quiet looks hung, and
			// the subject is what tells an operator it is working on real mail
			// rather than spinning on the same row.
			fmt.Printf("  [%3d] %s\n", p.Considered, truncateSubject(subject, 68))
		},
	})

	// A cancelled run is not a failure: every message was saved as it went, so
	// what finished is kept and the next run resumes from there.
	interrupted := errors.Is(err, context.Canceled)
	if err != nil && !interrupted {
		return fmt.Errorf("backfill: %w", err)
	}

	fmt.Printf("\n%s in %s: %d considered, %d classified, %d skipped, %d failed.\n",
		map[bool]string{true: "Stopped", false: "Done"}[interrupted],
		time.Since(start).Round(time.Second),
		p.Considered, p.Classified, p.Skipped, p.Failed)

	if *dryRun {
		// Pricing changes independently of this release, so report the measured
		// fixture estimate and leave the conversion to the current pricing page.
		tokens := p.Classified * 1300
		fmt.Printf("Estimated input if run: about %d tokens. Check current TypeSafe pricing, then re-run without --dry-run to classify.\n", tokens)
	} else if p.Considered == *limit {
		fmt.Printf("Hit the --limit. Run it again to continue; already-tagged mail is skipped.\n")
	}

	return nil
}

// resolveOrgID accepts either an organization id or the owner's email, because
// an operator reaching for this has one or the other and rarely both.
func resolveOrgID(ctx context.Context, c *conn, ref string) (uuid.UUID, error) {
	if id, err := uuid.Parse(ref); err == nil {
		return id, nil
	}
	var id uuid.UUID
	err := c.db.QueryRow(ctx, `
		SELECT o.id FROM organizations o
		JOIN users u ON u.id = o.owner_user_id
		WHERE LOWER(u.email) = LOWER($1)
		LIMIT 1`, ref).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("no workspace found for %q. Pass an organization id, or run `warmblyctl org list`.", ref)
	}
	return id, nil
}

func truncateSubject(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return "(no subject)"
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// runInboxTagFollowUps recomputes who owes whom a reply.
//
// The sweep makes no new model calls. Human-reply labels use classifications
// already stored by automatic tagging; outbound-last labels need only dates.
func runInboxTagFollowUps(ctx context.Context, args []string) error {
	fs := newFlagSet("inbox-tag follow-ups")
	org := fs.String("org", "", "organization id, or the owner's email address")
	days := fs.Int("days", 90, "how far back to consider threads")
	limit := fs.Int("limit", 2000, "most threads to sweep in this run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if strings.TrimSpace(*org) == "" {
		return errors.New("--org is required: a sweep is scoped to one workspace")
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	orgID, err := resolveOrgID(ctx, c, *org)
	if err != nil {
		return err
	}

	svc := inboxtag.NewService(
		nil, // no asker: this sweep never calls the model
		repository.NewInboxTagRepository(c.db.Pool),
		repository.NewTagCategoryStore(c.db.Pool),
		nil,
		true,
	)

	p, err := svc.SweepFollowUps(ctx, orgID, time.Now().AddDate(0, 0, -*days), *limit)
	if err != nil {
		return fmt.Errorf("follow-up sweep: %w", err)
	}

	fmt.Printf("Swept %d threads.\n", p.Threads)
	for _, label := range inboxtag.FollowUpLabels {
		if n := p.Labelled[label]; n > 0 {
			fmt.Printf("  %-18s %d\n", label, n)
		}
	}
	fmt.Printf("  %-18s %d\n", "(none needed)", p.Cleared)
	return nil
}
