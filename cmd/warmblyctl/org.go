package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/orgtransfer"
	"github.com/warmbly/warmbly/internal/models"
)

func runOrg(ctx context.Context, args []string) error {
	if len(args) == 0 {
		orgUsage(os.Stderr)
		return errors.New("`org` needs a subcommand. Pick one from the list above.")
	}

	switch args[0] {
	case "help", "-h", "--help":
		orgUsage(os.Stdout)
		return nil
	case "list":
		return runOrgList(ctx, args[1:])
	case "export":
		return runOrgExport(ctx, args[1:])
	case "import":
		return runOrgImport(ctx, args[1:])
	}

	orgUsage(os.Stderr)
	return fmt.Errorf("unknown subcommand `org %s`. Pick one from the list above.", args[0])
}

func orgUsage(w *os.File) {
	fmt.Fprint(w, "Move a workspace between instances.\n\nUsage:\n  warmblyctl org <subcommand> [flags]\n\nSubcommands:\n")
	for _, c := range commands {
		if !strings.HasPrefix(c.name, "org ") {
			continue
		}
		fmt.Fprintf(w, "  %-16s %s\n", strings.TrimPrefix(c.name, "org "), c.summary)
	}
	fmt.Fprint(w, "\nExamples:\n")
	for _, c := range commands {
		if strings.HasPrefix(c.name, "org ") {
			fmt.Fprintf(w, "  %s\n", c.example)
		}
	}
	fmt.Fprint(w, `
Data groups (--groups), comma separated. Omit to carry everything:
`)
	for _, g := range models.OrgDataGroupCatalog {
		note := ""
		if g.Required {
			note = "  (always included)"
		} else if g.Heavy {
			note = "  (large)"
		}
		fmt.Fprintf(w, "  %-12s %s%s\n", g.Key, g.Label, note)
	}
	fmt.Fprint(w, `
Credentials only travel when you pass --with-credentials, which seals them into
the archive under a passphrase you supply. Import the archive with the same
passphrase and mailboxes come up connected; without it they arrive needing a
reconnect. The passphrase is never stored on either instance, so losing it means
re-exporting.
`)
}

// ---------- org list ----------

func runOrgList(ctx context.Context, args []string) error {
	fs := newFlagSet("org list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	rows, err := c.db.Query(ctx, `
		SELECT o.id, o.name, u.email,
		       (SELECT count(*) FROM organization_members m WHERE m.organization_id = o.id),
		       (SELECT count(*) FROM email_accounts e WHERE e.organization_id = o.id),
		       (SELECT count(*) FROM contacts ct WHERE ct.organization_id = o.id)
		  FROM organizations o
		  LEFT JOIN users u ON u.id = o.owner_user_id
		 ORDER BY o.created_at
	`)
	if err != nil {
		return fmt.Errorf("listing organizations: %w", err)
	}
	defer rows.Close()

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tOWNER\tMEMBERS\tMAILBOXES\tCONTACTS")
	var found int
	for rows.Next() {
		var id uuid.UUID
		var name string
		var owner *string
		var members, mailboxes, contacts int64
		if err := rows.Scan(&id, &name, &owner, &members, &mailboxes, &contacts); err != nil {
			return err
		}
		ownerEmail := "(none)"
		if owner != nil {
			ownerEmail = *owner
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%d\n", id, name, ownerEmail, members, mailboxes, contacts)
		found++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if found == 0 {
		fmt.Println("\nThis instance has no organizations yet.")
	}
	return nil
}

// ---------- org export ----------

func runOrgExport(ctx context.Context, args []string) error {
	fs := newFlagSet("org export")
	orgRef := fs.String("org", "", "organization id, slug, or owner email (required)")
	out := fs.String("out", "", "file to write the archive to (required; - writes to stdout)")
	groupList := fs.String("groups", "", "comma-separated data groups to include (default: all)")
	withCreds := fs.Bool("with-credentials", false, "seal mailbox and integration credentials into the archive")
	passphraseStdin := fs.Bool("passphrase-stdin", false, "read the passphrase from stdin instead of prompting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if strings.TrimSpace(*orgRef) == "" {
		return errors.New("--org is required. Run `warmblyctl org list` to see what is on this instance.")
	}
	if strings.TrimSpace(*out) == "" {
		return errors.New("--out is required, for example --out ./workspace.warmbly.zip")
	}

	groups, err := parseGroups(*groupList)
	if err != nil {
		return err
	}

	var passphrase string
	if *withCreds {
		passphrase, err = readPassphrase(ctx, *passphraseStdin, "Passphrase for the archive's credentials")
		if err != nil {
			return err
		}
		if err := orgtransfer.ValidatePassphrase(passphrase); err != nil {
			return fmt.Errorf("%w. Nothing was written.", err)
		}
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	orgID, orgName, err := c.resolveOrg(ctx, *orgRef)
	if err != nil {
		return err
	}

	svc, err := c.orgTransferService(ctx)
	if err != nil {
		return err
	}

	// stdout is supported so the archive can be piped straight into ssh or a
	// bucket without ever touching this box's disk.
	var w *os.File
	if *out == "-" {
		w = os.Stdout
	} else {
		w, err = os.Create(*out)
		if err != nil {
			return fmt.Errorf("creating %s: %w", *out, err)
		}
		defer w.Close()
	}

	progress := newProgressPrinter(*out == "-")
	manifest, err := svc.ExportTo(ctx, orgID, orgtransfer.ExportOptions{
		Groups:     groups,
		Passphrase: passphrase,
	}, w, progress.report)
	if err != nil {
		return fmt.Errorf("exporting %s: %w", orgName, err)
	}
	progress.done()

	if *out == "-" {
		return nil
	}

	var total int64
	for _, t := range manifest.Tables {
		total += t.Rows
	}
	info, _ := w.Stat()

	fmt.Printf("\nExported %s\n", orgName)
	fmt.Printf("  File          %s", *out)
	if info != nil {
		fmt.Printf(" (%s)", humanBytes(info.Size()))
	}
	fmt.Println()
	fmt.Printf("  Rows          %d across %d tables\n", total, len(manifest.Tables))
	fmt.Printf("  Attachments   %d\n", len(manifest.Blobs))
	fmt.Printf("  Members       %d\n", len(manifest.Members))
	if manifest.Secrets != nil {
		fmt.Printf("  Credentials   sealed with your passphrase\n")
	} else {
		fmt.Printf("  Credentials   not included; mailboxes will need reconnecting after import\n")
	}

	printSteps("Import it on the other instance with:", []string{
		"warmblyctl org import --org <destination-org> --file " + *out +
			map[bool]string{true: " --passphrase-stdin", false: ""}[manifest.Secrets != nil],
	})
	return nil
}

// ---------- org import ----------

func runOrgImport(ctx context.Context, args []string) error {
	fs := newFlagSet("org import")
	orgRef := fs.String("org", "", "destination organization id, slug, or owner email (required)")
	file := fs.String("file", "", "archive to import (required)")
	groupList := fs.String("groups", "", "comma-separated data groups to apply (default: everything in the archive)")
	overwrite := fs.Bool("overwrite", false, "replace rows that already exist here instead of keeping them")
	passphraseStdin := fs.Bool("passphrase-stdin", false, "read the passphrase from stdin instead of prompting")
	withCreds := fs.Bool("with-credentials", false, "unseal the archive's credentials (prompts for the export passphrase)")
	dryRun := fs.Bool("dry-run", false, "report what would be applied and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := noExtraArgs(fs); err != nil {
		return err
	}
	if strings.TrimSpace(*orgRef) == "" {
		return errors.New("--org is required. Run `warmblyctl org list` to see what is on this instance.")
	}
	if strings.TrimSpace(*file) == "" {
		return errors.New("--file is required, for example --file ./workspace.warmbly.zip")
	}

	groups, err := parseGroups(*groupList)
	if err != nil {
		return err
	}

	f, err := os.Open(*file)
	if err != nil {
		return fmt.Errorf("opening %s: %w", *file, err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	archive := &fileArchive{f: f, size: stat.Size()}

	var passphrase string
	if *withCreds || *passphraseStdin {
		passphrase, err = readPassphrase(ctx, *passphraseStdin, "Passphrase the archive was exported with")
		if err != nil {
			return err
		}
	}

	c, err := connect(ctx)
	if err != nil {
		return err
	}
	defer c.close()

	orgID, orgName, err := c.resolveOrg(ctx, *orgRef)
	if err != nil {
		return err
	}

	svc, err := c.orgTransferService(ctx)
	if err != nil {
		return err
	}

	report, xerr := svc.Preflight(ctx, orgID, archive, passphrase)
	if xerr != nil {
		return errors.New(xerr.Message)
	}

	fmt.Printf("Archive\n")
	fmt.Printf("  Workspace     %s\n", report.Archive.OrganizationName)
	fmt.Printf("  Exported      %s from %s\n",
		report.Archive.ExportedAt.Format("2006-01-02 15:04 MST"), orNone(report.Archive.SourceInstance))
	fmt.Printf("  Rows          %d across %d tables\n", report.Archive.TotalRows(), len(report.Archive.RowCounts))
	fmt.Printf("  Attachments   %d\n", report.Archive.BlobCount)
	fmt.Printf("  Credentials   %s\n", credentialState(report))
	fmt.Printf("\nDestination     %s\n", orgName)

	if len(report.UnknownMembers) > 0 {
		lines := make([]string, 0, len(report.UnknownMembers))
		for _, m := range report.UnknownMembers {
			lines = append(lines, m.Email)
		}
		sort.Strings(lines)
		printSteps("These members have no account here, so their rows are reassigned to the workspace owner:", lines)
	}
	if len(report.Conflicts) > 0 {
		lines := make([]string, 0, len(report.Conflicts))
		for table, n := range report.Conflicts {
			lines = append(lines, fmt.Sprintf("%-32s %d", table, n))
		}
		sort.Strings(lines)
		verb := "kept as they are"
		if *overwrite {
			verb = "REPLACED, because --overwrite is set"
		}
		printSteps("Rows that already exist here will be "+verb+":", lines)
	}
	for _, w := range report.Warnings {
		warn("%s", w)
	}

	if *dryRun {
		fmt.Println("\nDry run: nothing was written.")
		return nil
	}

	conflict := models.OrgImportConflictSkip
	if *overwrite {
		conflict = models.OrgImportConflictOverwrite
	}

	owner, err := c.orgOwnerID(ctx, orgID)
	if err != nil {
		return err
	}

	fmt.Println()
	progress := newProgressPrinter(false)
	result, err := svc.ImportFrom(ctx, orgID, archive, orgtransfer.ImportOptions{
		Groups:      groups,
		Conflict:    conflict,
		Passphrase:  passphrase,
		ActorUserID: owner,
	}, progress.report)
	if err != nil {
		return fmt.Errorf("importing into %s: %w\nNothing was written: the import runs in one transaction.", orgName, err)
	}
	progress.done()

	var applied int64
	for _, n := range result.RowCounts {
		applied += n
	}
	fmt.Printf("\nImported %d rows into %s\n", applied, orgName)
	if result.SecretsApplied {
		fmt.Println("  Credentials were unsealed and re-keyed for this instance.")
	}
	for _, w := range result.Warnings {
		warn("%s", w)
	}

	printSteps("Next:", []string{
		"Check the mailboxes in the dashboard; any that arrived without credentials show as needing a reconnect.",
		"Workers are assigned by this instance, so a migrated mailbox is placed on its next scheduling pass.",
	})
	return nil
}

// ---------- helpers ----------

// fileArchive adapts an open file to what the zip reader needs.
type fileArchive struct {
	f    *os.File
	size int64
}

func (a *fileArchive) ReadAt(p []byte, off int64) (int, error) { return a.f.ReadAt(p, off) }
func (a *fileArchive) Size() int64                             { return a.size }

// parseGroups turns the --groups flag into a validated group list.
func parseGroups(raw string) ([]models.OrgDataGroup, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	known := make(map[string]models.OrgDataGroup, len(models.AllOrgDataGroups))
	for _, g := range models.AllOrgDataGroups {
		known[string(g)] = g
	}

	var out []models.OrgDataGroup
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		g, ok := known[name]
		if !ok {
			valid := make([]string, 0, len(models.AllOrgDataGroups))
			for _, k := range models.AllOrgDataGroups {
				valid = append(valid, string(k))
			}
			return nil, fmt.Errorf("unknown data group %q. Valid groups are: %s", name, strings.Join(valid, ", "))
		}
		out = append(out, g)
	}
	return out, nil
}

// readPassphrase prompts twice on a terminal, or reads stdin when told to. It
// reuses the password prompt so the terminal rules are identical everywhere.
func readPassphrase(ctx context.Context, fromStdin bool, what string) (string, error) {
	return readPassword(ctx, fromStdin, what)
}

func credentialState(r *models.OrgImportPreflight) string {
	switch {
	case !r.Archive.HasSecrets:
		return "not in this archive; mailboxes will need reconnecting"
	case r.SecretsUnsealed:
		return "sealed, and your passphrase opens them"
	default:
		return "sealed, but no passphrase was given; mailboxes will need reconnecting"
	}
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "an unnamed instance"
	}
	return s
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// progressPrinter keeps one line updated on stderr. It writes to stderr so
// `--out -` can pipe the archive on stdout with the progress still visible.
type progressPrinter struct {
	quiet bool
	last  string
}

func newProgressPrinter(quiet bool) *progressPrinter {
	return &progressPrinter{quiet: quiet}
}

func (p *progressPrinter) report(percent int, stage string) {
	if p.quiet || stage == p.last {
		return
	}
	p.last = stage
	fmt.Fprintf(os.Stderr, "\r\033[K  %3d%%  %s", percent, stage)
}

func (p *progressPrinter) done() {
	if p.quiet || p.last == "" {
		return
	}
	fmt.Fprint(os.Stderr, "\r\033[K")
}
