package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/warmbly/warmbly/internal/cli/api"
	"github.com/warmbly/warmbly/internal/cli/iostreams"
	"github.com/warmbly/warmbly/internal/cli/output"
)

// The store commands the spec table cannot express: building a cart from
// flags, the buy flow that confirms a price and charges, and watching an
// order's steps until it settles.

// minWatchInterval keeps `order watch` from polling the API in a tight loop.
var minWatchInterval = time.Second

func storeCommands(f *Factory) []*cobra.Command {
	return []*cobra.Command{newStoreQuoteCmd(f), newStoreBuyCmd(f)}
}

func storeOrderCommands(f *Factory) []*cobra.Command {
	return []*cobra.Command{newStoreOrderWatchCmd(f)}
}

// storeCart is the flags that name what to buy.
type storeCart struct {
	domains   []string
	years     int
	mailboxes []string
	providers []string
	first     string
	last      string
	admins    []string
	input     string
}

const storeCartHelp = `Name what to buy with flags, or pass the whole body with --input:

  --domain NAME[:YEARS]           register a new domain (repeatable)
  --mailbox ADDRESS[:FIRST:LAST]  create a mailbox, such as ada@getacme.com (repeatable)
  --provider google|microsoft     who hosts the mailboxes; DOMAIN=PROVIDER sets one domain
  --first-name, --last-name       the name on mailboxes that do not carry their own
  --admin ADDRESS                 the mailbox that administers its domain (one per domain)

Mailboxes can go on a domain registered in the same order, or on one the
workspace already has. --input takes {"items": [...]} as JSON, @file or - for
stdin, in the shape POST /v1/store/quotes documents.`

func (c *storeCart) bind(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringArrayVar(&c.domains, "domain", nil, "Register this domain, NAME[:YEARS] (repeatable)")
	fl.IntVar(&c.years, "years", 0, "Years for every --domain without its own (default: the shortest term)")
	fl.StringArrayVar(&c.mailboxes, "mailbox", nil, "Create this mailbox, ADDRESS[:FIRST:LAST] (repeatable)")
	fl.StringArrayVar(&c.providers, "provider", nil, "google or microsoft; DOMAIN=PROVIDER for one domain (repeatable)")
	fl.StringVar(&c.first, "first-name", "", "First name on mailboxes without their own")
	fl.StringVar(&c.last, "last-name", "", "Last name on mailboxes without their own")
	fl.StringArrayVar(&c.admins, "admin", nil, "The mailbox that administers its domain (repeatable, one per domain)")
	fl.StringVar(&c.input, "input", "", "Request body instead of the flags: JSON, @file, or - for stdin")
}

type storeNewMailbox struct {
	LocalPart string `json:"local_part"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Admin     bool   `json:"admin,omitempty"`
}

type storeQuoteItem struct {
	Type      string            `json:"type"`
	Domain    string            `json:"domain"`
	Years     int               `json:"years,omitempty"`
	Provider  string            `json:"provider,omitempty"`
	Mailboxes []storeNewMailbox `json:"mailboxes,omitempty"`
}

// body is the POST /store/quotes payload: --input as given, or the items the flags name.
func (c *storeCart) body(stdin io.Reader) ([]byte, error) {
	flagsUsed := len(c.domains)+len(c.mailboxes)+len(c.providers)+len(c.admins) > 0 || c.years != 0 || c.first != "" || c.last != ""
	raw, err := bodyFromArg(stdin, c.input)
	if err != nil {
		return nil, err
	}
	if raw != nil {
		if flagsUsed {
			return nil, fmt.Errorf("pass --input or the cart flags, not both")
		}
		if !json.Valid(raw) {
			return nil, fmt.Errorf("the request body is not valid JSON")
		}
		return raw, nil
	}
	items, err := c.items()
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"items": items})
}

func (c *storeCart) items() ([]storeQuoteItem, error) {
	if len(c.domains) == 0 && len(c.mailboxes) == 0 {
		return nil, fmt.Errorf("name something to buy: --domain, --mailbox, or a body with --input.\n\n%s", storeCartHelp)
	}

	defaultProvider := ""
	providerFor := map[string]string{}
	for _, p := range c.providers {
		domain, provider, scoped := strings.Cut(p, "=")
		if !scoped {
			provider, domain = domain, ""
		}
		provider = strings.ToLower(strings.TrimSpace(provider))
		if provider != "google" && provider != "microsoft" {
			return nil, fmt.Errorf("--provider %q: the provider is google or microsoft", p)
		}
		if domain == "" {
			defaultProvider = provider
		} else {
			providerFor[cleanDomain(domain)] = provider
		}
	}

	admins := map[string]bool{}
	for _, a := range c.admins {
		admins[strings.ToLower(strings.TrimSpace(a))] = true
	}

	// Items keep the order domains were named in, each domain's mailboxes right after it.
	var order []string
	seen := map[string]bool{}
	note := func(domain string) {
		if !seen[domain] {
			seen[domain] = true
			order = append(order, domain)
		}
	}
	register := map[string]int{}
	for _, d := range c.domains {
		name, yearsRaw, hasYears := strings.Cut(d, ":")
		name = cleanDomain(name)
		if name == "" {
			return nil, fmt.Errorf("--domain %q has no domain name", d)
		}
		years := c.years
		if hasYears {
			n, err := strconv.Atoi(strings.TrimSpace(yearsRaw))
			if err != nil || n < 1 {
				return nil, fmt.Errorf("--domain %q: the years after the colon must be a whole number, 1 or more", d)
			}
			years = n
		}
		if _, dup := register[name]; dup {
			return nil, fmt.Errorf("--domain %s is named twice", name)
		}
		register[name] = years
		note(name)
	}

	mailboxes := map[string][]storeNewMailbox{}
	addresses := map[string]bool{}
	for _, m := range c.mailboxes {
		parts := strings.Split(m, ":")
		address := strings.ToLower(strings.TrimSpace(parts[0]))
		local, domain, ok := strings.Cut(address, "@")
		domain = cleanDomain(domain)
		if !ok || local == "" || domain == "" {
			return nil, fmt.Errorf("--mailbox %q: start with the full address, such as ada@getacme.com", m)
		}
		first, last := c.first, c.last
		switch len(parts) {
		case 1:
		case 3:
			first, last = parts[1], parts[2]
		default:
			return nil, fmt.Errorf("--mailbox %q: use ADDRESS or ADDRESS:FIRST:LAST", m)
		}
		first, last = strings.TrimSpace(first), strings.TrimSpace(last)
		if first == "" || last == "" {
			return nil, fmt.Errorf("--mailbox %s needs a name: add :FIRST:LAST, or pass --first-name and --last-name", address)
		}
		if addresses[local+"@"+domain] {
			return nil, fmt.Errorf("--mailbox %s is named twice", address)
		}
		addresses[local+"@"+domain] = true
		mailboxes[domain] = append(mailboxes[domain], storeNewMailbox{LocalPart: local, FirstName: first, LastName: last, Admin: admins[local+"@"+domain]})
		note(domain)
	}
	for a := range admins {
		if !addresses[a] {
			return nil, fmt.Errorf("--admin %s is not one of the --mailbox addresses", a)
		}
	}

	var items []storeQuoteItem
	for _, domain := range order {
		if years, ok := register[domain]; ok {
			items = append(items, storeQuoteItem{Type: "domain", Domain: domain, Years: years})
		}
		if len(mailboxes[domain]) == 0 {
			continue
		}
		provider := providerFor[domain]
		if provider == "" {
			provider = defaultProvider
		}
		if provider == "" {
			return nil, fmt.Errorf("the mailboxes on %s need a provider: pass --provider google or --provider microsoft", domain)
		}
		items = append(items, storeQuoteItem{Type: "mailboxes", Domain: domain, Provider: provider, Mailboxes: mailboxes[domain]})
	}
	return items, nil
}

func cleanDomain(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// The parts of a quote the CLI prints and acts on.
type storeQuote struct {
	ID                        string          `json:"id"`
	ExpiresAt                 time.Time       `json:"expires_at"`
	Lines                     []storeQuoteRow `json:"lines"`
	DueNowMicros              int64           `json:"due_now_micros"`
	RecurringMonthlyMicros    int64           `json:"recurring_monthly_micros"`
	YearlyDomainRenewalMicros int64           `json:"yearly_domain_renewal_micros"`
	Payment                   struct {
		BalanceMicros      int64 `json:"balance_micros"`
		FromBalanceMicros  int64 `json:"from_balance_micros"`
		CardChargeMicros   int64 `json:"card_charge_micros"`
		NeedsPaymentMethod bool  `json:"needs_payment_method"`
		Card               *struct {
			Brand string `json:"brand"`
			Last4 string `json:"last4"`
		} `json:"card"`
	} `json:"payment"`
}

type storeQuoteRow struct {
	Type              string  `json:"type"`
	Domain            string  `json:"domain"`
	Years             int     `json:"years"`
	Provider          string  `json:"provider"`
	Count             int     `json:"count"`
	AmountMicros      int64   `json:"amount_micros"`
	RenewMicros       int64   `json:"renew_micros"`
	MonthlyMicros     int64   `json:"monthly_micros"`
	AdminEmail        *string `json:"admin_email"`
	NameserversChange bool    `json:"nameservers_change"`
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// printQuote writes the itemised lines and the totals a buyer agrees to.
func printQuote(w io.Writer, s *iostreams.IOStreams, q *storeQuote) {
	fmt.Fprintf(w, "%s %s\n", s.Bold("Quote"), s.Gray(q.ID+", prices held until "+q.ExpiresAt.Local().Format("15:04")))
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, l := range q.Lines {
		switch l.Type {
		case "domain":
			then := ""
			if l.RenewMicros > 0 {
				then = "then " + output.Dollars(l.RenewMicros) + "/year"
			}
			fmt.Fprintf(tw, "  %s\tregister for %s\t%s\t%s\n", l.Domain, plural(l.Years, "year", "years"), output.Dollars(l.AmountMicros), then)
		case "mailboxes":
			then := ""
			if l.MonthlyMicros > 0 {
				then = "then " + output.Dollars(l.MonthlyMicros) + "/month"
			}
			fmt.Fprintf(tw, "  %s\t%s %s\t%s\t%s\n", l.Domain, plural(l.Count, "mailbox", "mailboxes"), l.Provider, output.Dollars(l.AmountMicros), then)
			if l.AdminEmail != nil && *l.AdminEmail != "" {
				fmt.Fprintf(tw, "  \t%s\t\t\n", s.Gray("admin: "+*l.AdminEmail))
			}
			if l.NameserversChange {
				fmt.Fprintf(tw, "  \t%s\t\t\n", s.Gray("set new nameservers for the domain after the order"))
			}
		default:
			fmt.Fprintf(tw, "  %s\t%s\t%s\t\n", l.Domain, l.Type, output.Dollars(l.AmountMicros))
		}
	}
	_ = tw.Flush()

	fmt.Fprintf(w, "%s %s\n", s.Bold("Due now:"), s.Bold(output.Dollars(q.DueNowMicros)))
	p := q.Payment
	if q.DueNowMicros > 0 {
		var from []string
		if p.FromBalanceMicros > 0 {
			from = append(from, output.Dollars(p.FromBalanceMicros)+" from the workspace balance")
		}
		if p.CardChargeMicros > 0 {
			card := "the card on file"
			if p.Card != nil {
				card = p.Card.Brand + " ending " + p.Card.Last4
			}
			from = append(from, output.Dollars(p.CardChargeMicros)+" charged to "+card)
		}
		if len(from) > 0 {
			fmt.Fprintf(w, "  %s\n", s.Gray(strings.Join(from, ", ")))
		}
		if p.NeedsPaymentMethod {
			fmt.Fprintf(w, "  %s\n", s.Yellow("The balance does not cover this and there is no card on file."))
		}
	}
	if q.RecurringMonthlyMicros > 0 {
		fmt.Fprintf(w, "Then monthly: %s for mailboxes\n", output.Dollars(q.RecurringMonthlyMicros))
	}
	if q.YearlyDomainRenewalMicros > 0 {
		fmt.Fprintf(w, "Domain renewals: %s a year\n", output.Dollars(q.YearlyDomainRenewalMicros))
	}
}

func createQuote(ctx context.Context, client *api.Client, body []byte) (*storeQuote, []byte, error) {
	resp, err := client.Do(ctx, api.Request{Method: http.MethodPost, Path: "/store/quotes", Body: body})
	if err != nil {
		return nil, nil, err
	}
	var q storeQuote
	if err := json.Unmarshal(resp.Body, &q); err != nil || q.ID == "" {
		return nil, nil, fmt.Errorf("the API answered the quote with something unexpected")
	}
	return &q, resp.Body, nil
}

func newStoreQuoteCmd(f *Factory) *cobra.Command {
	var cart storeCart
	cmd := &cobra.Command{
		Use:   "quote",
		Short: "Price a cart of domains and mailboxes without buying it",
		Long:  "Price a cart without buying anything. The quote holds its prices for a few\nminutes; `warmbly store buy` with the same flags quotes again and buys.\n\n" + storeCartHelp,
		Example: `  $ warmbly store quote --domain getacme.com --mailbox ada@getacme.com --mailbox grace@getacme.com \
      --provider google --first-name Ada --last-name Lovelace
  $ warmbly store quote --mailbox ada@acme.com:Ada:Lovelace --provider microsoft
  $ warmbly store quote --input @cart.json --json`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			body, err := cart.body(f.IO.In)
			if err != nil {
				return err
			}
			client, err := f.Client()
			if err != nil {
				return err
			}
			q, raw, err := createQuote(c.Context(), client, body)
			if err != nil {
				return err
			}
			printer := f.Printer()
			if printer.JSON || printer.Template != "" || !f.IO.IsStdoutTTY() {
				return printer.Print(raw, output.Table{})
			}
			printQuote(f.IO.Out, f.IO, q)
			return nil
		},
	}
	cart.bind(cmd)
	return cmd
}

// storeBuy is one buy: quote, agree, order, and agree again if the price moves.
type storeBuy struct {
	client   *api.Client
	io       *iostreams.IOStreams
	yes      bool
	ask      func(question string) (bool, error)
	forward  string
	idemKey  string
	maxTries int
}

// run places the order and returns the order the API answered with.
func (b *storeBuy) run(ctx context.Context, body []byte) ([]byte, error) {
	q, _, err := createQuote(ctx, b.client, body)
	if err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		printQuote(b.io.ErrOut, b.io, q)
		if !b.yes {
			ok, aerr := b.ask(fmt.Sprintf("Buy this and charge %s now?", output.Dollars(q.DueNowMicros)))
			if aerr != nil {
				return nil, aerr
			}
			if !ok {
				return nil, errCancelled
			}
		}

		order := map[string]any{"quote_id": q.ID, "expected_due_now_micros": q.DueNowMicros}
		if b.forward != "" {
			order["forwarding_url"] = b.forward
		}
		payload, _ := json.Marshal(order)
		key := b.idemKey
		if key != "" && attempt > 1 {
			// A new total is a different request, which must not replay the first answer.
			key = fmt.Sprintf("%s-%d", key, attempt)
		}
		resp, err := b.client.Do(ctx, api.Request{Method: http.MethodPost, Path: "/store/orders", Body: payload, IdempotencyKey: key})
		if err == nil {
			return resp.Body, nil
		}

		var apiErr *api.Error
		if !errors.As(err, &apiErr) {
			return nil, err
		}
		switch apiErr.Code {
		case "price_changed":
			var env struct {
				Details struct {
					Quote *storeQuote `json:"quote"`
				} `json:"details"`
			}
			if json.Unmarshal([]byte(apiErr.Body), &env) != nil || env.Details.Quote == nil || env.Details.Quote.ID == "" {
				return nil, err
			}
			fresh := env.Details.Quote
			if b.yes {
				return nil, fmt.Errorf("the price changed after the quote: due now is %s, not %s. Nothing was charged.\nRun the command again to buy at the new price", output.Dollars(fresh.DueNowMicros), output.Dollars(q.DueNowMicros))
			}
			if attempt >= b.maxTries {
				return nil, fmt.Errorf("the price kept changing while you were confirming. Nothing was charged; try again in a minute")
			}
			fmt.Fprintf(b.io.ErrOut, "\n%s The price changed from %s to %s. Nothing was charged. Here is the new quote:\n\n",
				b.io.Yellow("!"), output.Dollars(q.DueNowMicros), output.Dollars(fresh.DueNowMicros))
			q = fresh
		case "payment_method_required":
			var env struct {
				SetupURL string `json:"setup_url"`
			}
			_ = json.Unmarshal([]byte(apiErr.Body), &env)
			where := "in the dashboard's billing settings (`warmbly browse billing`)"
			if env.SetupURL != "" {
				where = "at " + env.SetupURL
			}
			return nil, fmt.Errorf("the workspace balance does not cover this order and there is no card to charge. Nothing was charged.\nAdd a card %s, then run the command again (request %s)", where, apiErr.RequestID)
		default:
			return nil, err
		}
	}
}

func newStoreBuyCmd(f *Factory) *cobra.Command {
	var (
		cart    storeCart
		forward string
		idemKey string
	)
	cmd := &cobra.Command{
		Use:   "buy",
		Short: "Buy domains and mailboxes, charging the workspace balance",
		Long: "Quote a cart, show what it costs now and every month after, and buy it once\nyou agree. The workspace balance pays first, then the card on file.\n\n" +
			"This command spends money. It asks before buying, and refuses with no\nterminal to ask on unless --yes is passed. If the price moves between the\nquote and the order, it shows the new one and asks again; with --yes it\nstops instead, and nothing is charged.\n\n" +
			"Buying needs MANAGE_STORE, and the member behind the key must hold\nmanage_billing. `warmbly store order watch` follows the order afterwards.\n\n" + storeCartHelp,
		Example: `  $ warmbly store buy --domain getacme.com --mailbox ada@getacme.com --provider google \
      --first-name Ada --last-name Lovelace --forwarding-url https://acme.com
  $ warmbly store buy --input @cart.json --yes --json`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			body, err := cart.body(f.IO.In)
			if err != nil {
				return err
			}
			if !f.AssumeYes && !f.IO.IsStdinTTY() {
				return fmt.Errorf("`warmbly store buy` charges the workspace balance, and there is no terminal to confirm on.\nCheck the price with `warmbly store quote`, then pass --yes to buy")
			}
			client, err := f.Client()
			if err != nil {
				return err
			}
			b := &storeBuy{
				client: client, io: f.IO, yes: f.AssumeYes, forward: strings.TrimSpace(forward), idemKey: idemKey, maxTries: 3,
				ask: func(q string) (bool, error) { return f.IO.Confirm(f.IO.Yellow("! ")+q, false) },
			}
			raw, err := b.run(c.Context(), body)
			if err != nil {
				return err
			}
			printer := f.Printer()
			if printer.JSON || printer.Template != "" || !f.IO.IsStdoutTTY() {
				return printer.Print(raw, output.Table{})
			}
			var o struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			}
			_ = json.Unmarshal(raw, &o)
			f.IO.Printf("%s Ordered (%s). Order %s\n", f.IO.Tick(), o.Status, o.ID)
			f.IO.Printf("%s\n", f.IO.Gray("Follow it with `warmbly store order watch "+o.ID+"`. Mailbox setup can take a few hours."))
			return nil
		},
	}
	cart.bind(cmd)
	cmd.Flags().StringVar(&forward, "forwarding-url", "", "Send visitors of newly registered domains to this https URL")
	cmd.Flags().StringVar(&idemKey, "idempotency-key", "", "Idempotency-Key header for the order")
	return cmd
}

// storeOrder is what the watch draws.
type storeOrder struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Steps  []struct {
		Key    string     `json:"key"`
		Status string     `json:"status"`
		At     *time.Time `json:"at"`
	} `json:"steps"`
}

func (o *storeOrder) settled() bool { return o.Status != "processing" && o.Status != "charging" }

var storeStepLabels = map[string]string{
	"register":   "Register the domains",
	"dns":        "Set up DNS",
	"mail_auth":  "Set up mail authentication",
	"mailboxes":  "Create the mailboxes",
	"delegation": "Connect the mailboxes to Warmbly",
	"ready":      "Ready",
}

// orderLines renders the step track, each line fitted to width.
func orderLines(s *iostreams.IOStreams, o *storeOrder, width int, now time.Time) []string {
	fit := func(text string, used int) string {
		limit := width - used - 1
		if limit < 10 {
			limit = 10
		}
		if r := []rune(text); len(r) > limit {
			return string(r[:limit-1]) + "…"
		}
		return text
	}
	lines := []string{fit(fmt.Sprintf("Order %s  %s  (checked %s)", o.ID, o.Status, now.Local().Format("15:04:05")), 0)}
	for _, st := range o.Steps {
		icon := s.Gray("-")
		switch st.Status {
		case "done":
			icon = s.Tick()
		case "failed":
			icon = s.Cross()
		case "running":
			icon = s.Cyan("*")
		}
		label := storeStepLabels[st.Key]
		if label == "" {
			label = st.Key
		}
		text := label + "  " + st.Status
		if st.At != nil && st.Status != "pending" {
			text += "  " + st.At.Local().Format("15:04")
		}
		lines = append(lines, "  "+icon+" "+fit(text, 5))
	}
	return lines
}

func newStoreOrderWatchCmd(f *Factory) *cobra.Command {
	var interval, timeout time.Duration
	cmd := &cobra.Command{
		Use:   "watch <id>",
		Short: "Follow an order's steps until it completes or fails",
		Long: `Check the order every --interval and redraw its steps until it is no longer
charging or processing. Creating and connecting mailboxes can take hours, so
this stops after --timeout (0 waits as long as it takes). Ctrl-C stops
watching at any time; the order carries on without it.

Exits 0 when the order completes, and 1 when it fails, partly fails, or the
watch stops before it settles.

Arguments:
  <id>  The order's id`,
		Example: "  $ warmbly store order watch ORDER_ID\n  $ warmbly store order watch ORDER_ID --interval 30s --timeout 6h",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("<id> cannot be empty")
			}
			if interval < minWatchInterval {
				interval = minWatchInterval
			}
			client, err := f.Client()
			if err != nil {
				return err
			}
			return watchOrder(c.Context(), f, client, id, interval, timeout)
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 5*time.Second, "How often to check the order")
	cmd.Flags().DurationVar(&timeout, "timeout", time.Hour, "Stop watching after this long; 0 for no limit")
	return cmd
}

func watchOrder(ctx context.Context, f *Factory, client *api.Client, id string, interval, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	printer := f.Printer()
	machine := printer.JSON || printer.Template != "" || !f.IO.IsStdoutTTY()
	path := "/store/orders/" + id

	drawn := 0
	last := ""
	var raw []byte
	var o storeOrder
	for {
		resp, err := client.Do(ctx, api.Request{Method: http.MethodGet, Path: path})
		if err != nil {
			if ctx.Err() == nil {
				return err
			}
		} else {
			raw = resp.Body
			if jerr := json.Unmarshal(raw, &o); jerr != nil {
				return fmt.Errorf("the API answered the order with something unexpected")
			}
			lines := orderLines(f.IO, &o, f.IO.TerminalWidth(), time.Now())
			if machine {
				// No redraw off a terminal: one progress line on stderr each time a step moves.
				if state := strings.Join(lines[1:], "|") + o.Status; state != last {
					last = state
					fmt.Fprintln(f.IO.ErrOut, orderProgress(&o))
				}
			} else {
				if drawn > 0 {
					fmt.Fprintf(f.IO.Out, "\033[%dA", drawn)
				}
				for _, l := range lines {
					fmt.Fprintf(f.IO.Out, "\r\033[K%s\n", l)
				}
				drawn = len(lines)
			}
			if o.settled() {
				break
			}
		}

		select {
		case <-ctx.Done():
		case <-time.After(interval):
			continue
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("order %s is still %s after %s. It carries on; run `warmbly store order watch %s` again later", id, o.Status, timeout, id)
		}
		fmt.Fprintf(f.IO.ErrOut, "\nStopped watching. Order %s carries on; `warmbly store order view %s` shows where it is.\n", id, id)
		return errSilent
	}

	if machine {
		if err := printer.Print(raw, output.Table{}); err != nil {
			return err
		}
	}
	switch o.Status {
	case "completed":
		if !machine {
			f.IO.Printf("%s Order complete.\n", f.IO.Tick())
		}
		return nil
	case "partially_failed":
		fmt.Fprintf(f.IO.ErrOut, "%s Part of order %s failed. `warmbly store order view %s` lists what was set up.\n", f.IO.Cross(), id, id)
	default:
		fmt.Fprintf(f.IO.ErrOut, "%s Order %s ended %s.\n", f.IO.Cross(), id, o.Status)
	}
	return errSilent
}

// orderProgress is the one-line form of the step track, for logs.
func orderProgress(o *storeOrder) string {
	parts := make([]string, 0, len(o.Steps))
	for _, st := range o.Steps {
		parts = append(parts, st.Key+"="+st.Status)
	}
	return fmt.Sprintf("%s %s %s %s", time.Now().Format("15:04:05"), o.ID, o.Status, strings.Join(parts, " "))
}
