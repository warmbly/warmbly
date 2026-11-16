package toolmigrate

import (
	"log"

	"github.com/warmbly/warmbly/internal/config"
	source "github.com/warmbly/warmbly/internal/pkg/outreachsource"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/apollo"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/instantly"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/lemlist"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/mailshake"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/quickmail"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/replyio"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/saleshandy"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/smartlead"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/snovio"
	"github.com/warmbly/warmbly/internal/pkg/outreachsource/woodpecker"
)

// Registry is every tool this instance migrates from, in the picker's order.
func Registry() *source.Registry {
	sl := smartlead.New()
	// Dev only: the sandbox plays Smartlead, so a migration runs with no Smartlead account.
	if u := config.MailvendorSandboxURL(); u != "" {
		log.Printf("tool migrations: Smartlead uses the sandbox mock at %s/smartlead", u)
		sl = smartlead.NewWithBaseURL(u + "/smartlead")
	}
	return source.NewRegistry(
		sl,
		instantly.New(),
		lemlist.New(),
		replyio.New(),
		woodpecker.New(),
		mailshake.New(),
		saleshandy.New(),
		snovio.New(),
		apollo.New(),
		quickmail.New(),
	)
}
