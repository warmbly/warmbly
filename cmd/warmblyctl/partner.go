package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/warmbly/warmbly/internal/app/partner"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Resellers run workspaces through /v1/partner, which answers nothing until a partner exists here.

func runPartner(ctx context.Context, args []string) error {
	if len(args) == 0 {
		partnerUsage(os.Stderr)
		return errors.New("`partner` needs a subcommand. Pick one from the list above.")
	}
	switch args[0] {
	case "help", "-h", "--help":
		partnerUsage(os.Stdout)
		return nil
	case "create":
		return runPartnerCreate(ctx, args[1:])
	case "list":
		return runPartnerList(ctx, args[1:])
	case "key":
		return runPartnerKey(ctx, args[1:])
	}
	partnerUsage(os.Stderr)
	return fmt.Errorf("unknown subcommand `partner %s`. Pick one from the list above.", args[0])
}

func partnerUsage(w *os.File) {
	fmt.Fprint(w, "Manage resellers that create and run workspaces through the partner API.\n\nUsage:\n  warmblyctl partner <subcommand> [flags]\n\nSubcommands:\n")
	for _, c := range commands {
		if !strings.HasPrefix(c.name, "partner ") {
			continue
		}
		fmt.Fprintf(w, "  %-28s %s\n    %s\n", strings.TrimPrefix(c.name, "partner "), c.summary, c.example)
	}
}

func partnerService(ctx context.Context) (*partner.Service, func(), error) {
	c, err := connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	return partner.NewService(partner.Deps{Repo: repository.NewPartnerRepository(c.db.Pool)}), c.close, nil
}

func runPartnerCreate(ctx context.Context, args []string) error {
	fs := newFlagSet("partner create")
	slug := fs.String("slug", "", "short identifier, lowercase letters, digits and dashes")
	name := fs.String("name", "", "the partner's name")
	display := fs.String("display-name", "", `what members of its workspaces read, as in "Billed through <display name>" (default: --name)`)
	hosts := fs.String("allowed-hosts", "", "comma-separated hosts a partner-supplied return URL may point at")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if *slug == "" || *name == "" {
		return errors.New("--slug and --name are required")
	}
	svc, done, err := partnerService(ctx)
	if err != nil {
		return err
	}
	defer done()
	var allowed []string
	for _, h := range strings.Split(*hosts, ",") {
		if h = strings.TrimSpace(h); h != "" {
			allowed = append(allowed, h)
		}
	}
	p, xerr := svc.CreatePartner(ctx, partner.CreatePartnerInput{Slug: *slug, Name: *name, DisplayName: *display, AllowedHosts: allowed}, nil)
	if xerr != nil {
		return errors.New(xerr.Message)
	}
	fmt.Printf("Created partner %s (%s), id %s.\nIssue a key with: warmblyctl partner key --partner %s --mode test\n", p.Name, p.Slug, p.ID, p.Slug)
	return nil
}

func runPartnerList(ctx context.Context, args []string) error {
	fs := newFlagSet("partner list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	svc, done, err := partnerService(ctx)
	if err != nil {
		return err
	}
	defer done()
	list, err := svc.Repo.ListPartners(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("No partners. The partner API is off on this instance.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tNAME\tDISPLAY NAME\tSTATUS\tWEBHOOK\tID")
	for _, p := range list {
		hook := "-"
		if p.WebhookURL != nil {
			hook = *p.WebhookURL
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Slug, p.Name, p.DisplayName, p.Status, hook, p.ID)
	}
	return tw.Flush()
}

func runPartnerKey(ctx context.Context, args []string) error {
	fs := newFlagSet("partner key")
	slug := fs.String("partner", "", "the partner's slug")
	mode := fs.String("mode", "test", "live or test: which workspaces the key reaches")
	name := fs.String("name", "", "a label for the key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if *slug == "" {
		return errors.New("--partner is required")
	}
	svc, done, err := partnerService(ctx)
	if err != nil {
		return err
	}
	defer done()
	p, err := svc.Repo.GetPartnerBySlug(ctx, strings.ToLower(strings.TrimSpace(*slug)))
	if err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("no partner with slug %q. `warmblyctl partner list` shows them.", *slug)
	}
	k, xerr := svc.CreateKey(ctx, p.ID, models.PartnerMode(*mode), *name, nil)
	if xerr != nil {
		return errors.New(xerr.Message)
	}
	fmt.Fprintf(os.Stderr, "Created a %s key for %s. It is shown once; store it now.\n", k.Mode, p.Name)
	fmt.Println(k.Secret)
	return nil
}
