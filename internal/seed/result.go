package seed

import (
	"fmt"
	"io"
	"strings"
)

// Result describes everything the seed produced that a human running the
// command will want to know: credentials, the API key secret (only available
// once), and which IDs were created so they can be referenced from manual
// testing.
type Result struct {
	Password string

	Users         []SeededUser
	Organizations []SeededOrg
	Workers       []SeededWorker
	APIKeySecret  string
	APIKeyPrefix  string
}

type SeededUser struct {
	Role  string
	Email string
	ID    string
}

type SeededOrg struct {
	Name      string
	Slug      string
	ID        string
	Plan      string
	Mailboxes int
	Campaigns int
}

type SeededWorker struct {
	Name string
	Tier string
	Type string
	ID   string
}

func (r *Result) Print(w io.Writer) {
	b := &strings.Builder{}
	b.WriteString("\n")
	b.WriteString("======================================================================\n")
	b.WriteString("                       Warmbly dev seed complete\n")
	b.WriteString("======================================================================\n\n")
	fmt.Fprintf(b, "Password for every user:   %s\n\n", r.Password)

	b.WriteString("Users\n-----\n")
	for _, u := range r.Users {
		fmt.Fprintf(b, "  %-14s %-30s  (%s)\n", u.Role, u.Email, u.ID)
	}
	b.WriteString("\nOrganisations\n-------------\n")
	for _, o := range r.Organizations {
		fmt.Fprintf(b, "  %-12s slug=%s  plan=%s  mailboxes=%d  campaigns=%d\n",
			o.Name, o.Slug, o.Plan, o.Mailboxes, o.Campaigns)
	}
	b.WriteString("\nWorkers\n-------\n")
	for _, w := range r.Workers {
		fmt.Fprintf(b, "  %-22s tier=%-7s type=%-9s  (%s)\n", w.Name, w.Tier, w.Type, w.ID)
	}

	if r.APIKeySecret != "" {
		b.WriteString("\nAPI key (Acme owner, full access)\n---------------------------------\n")
		fmt.Fprintf(b, "  Prefix: %s\n  Secret: %s\n  (Stored as SHA-256 hash; secret is shown ONCE per seed run.)\n",
			r.APIKeyPrefix, r.APIKeySecret)
	}

	b.WriteString("\n======================================================================\n")
	io.WriteString(w, b.String())
}
