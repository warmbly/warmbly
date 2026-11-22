package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/store"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// StoreTools is the slice of the store the tools call, exactly as the REST handlers do.
type StoreTools interface {
	Status(ctx context.Context, orgID uuid.UUID) (*store.Status, error)
	Search(ctx context.Context, query string, tlds []string, exactOnly bool) (*store.SearchResult, error)
	ListOrders(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*store.Page[store.Order], error)
	GetOrder(ctx context.Context, orgID, id uuid.UUID) (*store.Order, error)
	ListDomains(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*store.Page[store.Domain], error)
	GetDomain(ctx context.Context, orgID, id uuid.UUID) (*store.Domain, error)
	ListMailboxes(ctx context.Context, orgID uuid.UUID, cursor string, limit int) (*store.Page[store.Mailbox], error)
	CreateQuote(ctx context.Context, orgID uuid.UUID, in store.QuoteRequest) (*store.Quote, error)
	PlaceOrder(ctx context.Context, orgID, userID uuid.UUID, in store.OrderRequest) (*store.Order, error)
	PatchDomain(ctx context.Context, orgID, id uuid.UUID, p store.DomainPatch) (*store.Domain, error)
	RenewNow(ctx context.Context, orgID, id uuid.UUID) (*store.Domain, error)
	CancelMailbox(ctx context.Context, orgID, id uuid.UUID) (*store.Mailbox, error)
	ResumeMailbox(ctx context.Context, orgID, id uuid.UUID) (*store.Mailbox, error)
	AddDNSRecord(ctx context.Context, orgID, id uuid.UUID, in store.DNSRecordInput) (*store.DNSRecord, error)
	DeleteDNSRecord(ctx context.Context, orgID, id, rid uuid.UUID) error
}

var _ StoreTools = (*store.Service)(nil)

// storeCharges is what a tool that charges or changes a charge needs from the member, like the REST route.
const storeCharges = models.PermManageEmails | models.PermManageBilling

// StoreToolNames are the store's tools, read and write.
var StoreToolNames = []string{
	"get_store_status", "search_store_domains", "list_store_orders", "get_store_order", "list_store_domains", "get_store_domain",
	"list_store_mailboxes", "create_store_quote", "place_store_order", "set_store_domain_auto_renew", "renew_store_domain",
	"cancel_store_mailbox", "resume_store_mailbox", "add_store_dns_record", "delete_store_dns_record",
}

// RegisterStoreTools adds the domains and mailboxes tools once the store is built.
// Reads take READ_STORE; changes take MANAGE_STORE, and a charge also holds the
// member to manage_billing and a session to a recent confirmation. Transfer-out
// and sign-in answer with a secret, so they are not tools.
func RegisterStoreTools(r *Registry, s StoreTools, auditSvc audit.AuditService) {
	if r == nil || s == nil {
		return
	}
	d := Deps{Audit: auditSvc}
	read := func(name, desc string, schema map[string]any, h Handler) {
		r.Register(Tool{Name: name, Description: desc, InputSchema: schema, Risk: generation.RiskRead,
			RequiredOrgPerm: models.PermManageEmails, RequiredAPIPerm: models.APIPermReadStore, Handler: h})
	}
	page := objectSchema(map[string]any{
		"cursor": strProp("Opaque cursor from the previous page's pagination.next_cursor."),
		"limit":  intProp("How many to return, 1 to 100 (default 25)."),
	})
	idArg := func(name, desc string) map[string]any {
		return objectSchema(map[string]any{name: strProp(desc)}, name)
	}

	read("get_store_status", "Whether this workspace can buy domains and Google or Microsoft mailboxes, the monthly mailbox price, the workspace balance and saved card, and whether a paid plan is active (buying needs one).",
		objectSchema(map[string]any{}), func(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
			return storeResult(s.Status(ctx, inv.OrgID))
		})
	read("search_store_domains", "Search domains to buy by keyword or name, with availability, the price for the first term and the yearly renewal price (micro-USD).",
		objectSchema(map[string]any{
			"query": strProp("A word or a domain name, e.g. acme or getacme.com."),
			"tlds":  arrProp("Endings to search, e.g. [\"com\", \"io\"]. Optional.", map[string]any{"type": "string"}),
		}, "query"), func(ctx context.Context, _ Invocation, args json.RawMessage) (string, error) {
			in, err := decodeArgs[struct {
				Query string   `json:"query"`
				TLDs  []string `json:"tlds"`
			}](args)
			if err != nil {
				return "", err
			}
			return storeResult(s.Search(ctx, in.Query, in.TLDs, false))
		})
	read("list_store_orders", "List the workspace's domain and mailbox orders, newest first, with status, totals, refunds and setup steps.",
		page, func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			p, err := decodeArgs[storePageArgs](args)
			if err != nil {
				return "", err
			}
			return storeResult(s.ListOrders(ctx, inv.OrgID, p.Cursor, p.Limit))
		})
	read("get_store_order", "Read one order: its status, each setup step, what was charged and refunded, and the domains and mailboxes it created.",
		idArg("order_id", "The order's id."), func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			id, err := idFrom(args, "order_id")
			if err != nil {
				return "", err
			}
			return storeResult(s.GetOrder(ctx, inv.OrgID, id))
		})
	read("list_store_domains", "List the workspace's purchased and connected domains with expiry, auto-renew, renewal price and mail records.",
		page, func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			p, err := decodeArgs[storePageArgs](args)
			if err != nil {
				return "", err
			}
			return storeResult(s.ListDomains(ctx, inv.OrgID, p.Cursor, p.Limit))
		})
	read("get_store_domain", "Read one purchased or connected domain with its DNS records.",
		idArg("domain_id", "The domain's id."), func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			id, err := idFrom(args, "domain_id")
			if err != nil {
				return "", err
			}
			return storeResult(s.GetDomain(ctx, inv.OrgID, id))
		})
	read("list_store_mailboxes", "List the workspace's purchased mailboxes with status, monthly price, paid-through date, pending cancellation and whether each is connected.",
		page, func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			p, err := decodeArgs[storePageArgs](args)
			if err != nil {
				return "", err
			}
			return storeResult(s.ListMailboxes(ctx, inv.OrgID, p.Cursor, p.Limit))
		})

	write := func(t Tool) {
		t.Risk = generation.RiskWrite
		t.RequiredAPIPerm = models.APIPermManageStore
		if t.RequiredOrgPerm == 0 {
			t.RequiredOrgPerm = models.PermManageEmails
		}
		r.Register(t)
	}
	write(Tool{
		Name: "create_store_quote",
		Description: "Price a cart of domains and mailboxes; nothing is charged. Each item is {\"type\":\"domain\",\"domain\":\"getacme.com\",\"years\":1} or " +
			"{\"type\":\"mailboxes\",\"domain\":\"getacme.com\",\"provider\":\"google\"|\"microsoft\",\"mailboxes\":[{\"local_part\":\"ada\",\"first_name\":\"Ada\",\"last_name\":\"Lovelace\"}]}. " +
			"Returns the quote id, each line, due_now_micros and the monthly total; show the person the total before ordering.",
		InputSchema: objectSchema(map[string]any{
			"items": arrProp("Up to 20 items.", map[string]any{"type": "object"}),
		}, "items"),
		Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			in, err := decodeArgs[store.QuoteRequest](args)
			if err != nil {
				return "", err
			}
			return storeResult(s.CreateQuote(ctx, inv.OrgID, in))
		},
	})
	write(Tool{
		Name: "place_store_order",
		Description: "Buy what a quote holds, charged to the workspace balance (the saved card tops up any shortfall). " +
			"expected_due_now_micros must be the total the person agreed to; if the price moved, nothing is charged and the new total comes back to confirm again. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"quote_id":                strProp("The id from create_store_quote."),
			"expected_due_now_micros": intProp("The quote's due_now_micros the person agreed to."),
			"forwarding_url":          strProp("Optional https:// address the bare domain forwards to."),
		}, "quote_id", "expected_due_now_micros"),
		RequiredOrgPerm: storeCharges, FreshAuth: true, AlwaysAsk: true,
		Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			in, err := decodeArgs[struct {
				QuoteID       string `json:"quote_id"`
				Expected      *int64 `json:"expected_due_now_micros"`
				ForwardingURL string `json:"forwarding_url"`
			}](args)
			if err != nil {
				return "", err
			}
			qid, err := parseUUIDArg(in.QuoteID)
			if err != nil || in.Expected == nil {
				return "", ErrInvalidArgs
			}
			o, err := s.PlaceOrder(ctx, inv.OrgID, inv.UserID, store.OrderRequest{QuoteID: qid, ExpectedDueNowMicros: in.Expected, ForwardingURL: in.ForwardingURL})
			if err != nil {
				return "", storeToolErr(err)
			}
			d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityStoreOrder, &o.ID, map[string]string{"due_now_usd": dollars(o.DueNowMicros)})
			return jsonResult(o)
		},
	})
	write(Tool{
		Name:        "set_store_domain_auto_renew",
		Description: "Turn a purchased domain's automatic yearly renewal on or off. Renewals are charged to the workspace balance. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"domain_id":  strProp("The domain's id."),
			"auto_renew": boolProp("true to renew every year, false to let it expire."),
		}, "domain_id", "auto_renew"),
		RequiredOrgPerm: storeCharges, FreshAuth: true, AlwaysAsk: true,
		Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			in, err := decodeArgs[struct {
				DomainID  string `json:"domain_id"`
				AutoRenew *bool  `json:"auto_renew"`
			}](args)
			if err != nil {
				return "", err
			}
			id, err := parseUUIDArg(in.DomainID)
			if err != nil || in.AutoRenew == nil {
				return "", ErrInvalidArgs
			}
			dom, err := s.PatchDomain(ctx, inv.OrgID, id, store.DomainPatch{AutoRenew: in.AutoRenew})
			if err != nil {
				return "", storeToolErr(err)
			}
			d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityStoreDomain, &dom.ID, map[string]string{"domain": dom.Name, "auto_renew": fmt.Sprint(*in.AutoRenew)})
			return jsonResult(dom)
		},
	})
	write(Tool{
		Name:            "renew_store_domain",
		Description:     "Renew a purchased domain for one more year now, charged at its current renewal price to the workspace balance. Requires user approval.",
		InputSchema:     idArg("domain_id", "The domain's id."),
		RequiredOrgPerm: storeCharges, FreshAuth: true, AlwaysAsk: true,
		Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			id, err := idFrom(args, "domain_id")
			if err != nil {
				return "", err
			}
			dom, err := s.RenewNow(ctx, inv.OrgID, id)
			if err != nil {
				return "", storeToolErr(err)
			}
			d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityStoreDomain, &dom.ID, map[string]string{"domain": dom.Name, "action": "renew"})
			return jsonResult(dom)
		},
	})
	write(Tool{
		Name:            "cancel_store_mailbox",
		Description:     "Cancel a purchased mailbox at the end of its paid month; it keeps working until then and nothing more is charged. Requires user approval.",
		InputSchema:     idArg("mailbox_id", "The purchased mailbox's id (from list_store_mailboxes)."),
		RequiredOrgPerm: storeCharges, FreshAuth: true, AlwaysAsk: true,
		Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			id, err := idFrom(args, "mailbox_id")
			if err != nil {
				return "", err
			}
			m, err := s.CancelMailbox(ctx, inv.OrgID, id)
			if err != nil {
				return "", storeToolErr(err)
			}
			d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityStoreMailbox, &m.ID, map[string]string{"email": m.Email})
			return jsonResult(m)
		},
	})
	write(Tool{
		Name:            "resume_store_mailbox",
		Description:     "Undo a purchased mailbox's pending cancellation; it bills monthly again. Requires user approval.",
		InputSchema:     idArg("mailbox_id", "The purchased mailbox's id."),
		RequiredOrgPerm: storeCharges, FreshAuth: true, AlwaysAsk: true,
		Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			id, err := idFrom(args, "mailbox_id")
			if err != nil {
				return "", err
			}
			m, err := s.ResumeMailbox(ctx, inv.OrgID, id)
			if err != nil {
				return "", storeToolErr(err)
			}
			d.logAudit(ctx, inv, models.AuditActionResume, models.AuditEntityStoreMailbox, &m.ID, map[string]string{"email": m.Email})
			return jsonResult(m)
		},
	})
	write(Tool{
		Name:        "add_store_dns_record",
		Description: "Add a DNS record to a purchased or connected domain whose DNS Warmbly hosts. Records Warmbly keeps for mail cannot be changed.",
		InputSchema: objectSchema(map[string]any{
			"domain_id": strProp("The domain's id."),
			"type":      enumProp("Record type.", "A", "AAAA", "CNAME", "TXT", "MX"),
			"name":      strProp("Name relative to the domain, @ for the domain itself."),
			"content":   strProp("The record's value."),
			"ttl":       intProp("Seconds; optional."),
			"priority":  intProp("MX priority; optional."),
		}, "domain_id", "type", "name", "content"),
		Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			in, err := decodeArgs[struct {
				DomainID string `json:"domain_id"`
				store.DNSRecordInput
			}](args)
			if err != nil {
				return "", err
			}
			id, err := parseUUIDArg(in.DomainID)
			if err != nil {
				return "", err
			}
			rec, err := s.AddDNSRecord(ctx, inv.OrgID, id, in.DNSRecordInput)
			if err != nil {
				return "", storeToolErr(err)
			}
			d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityStoreDomain, &id, map[string]string{"dns_record": rec.Type + " " + rec.Name})
			return jsonResult(rec)
		},
	})
	write(Tool{
		Name:        "delete_store_dns_record",
		Description: "Delete one of the workspace's own DNS records on a store domain.",
		InputSchema: objectSchema(map[string]any{
			"domain_id": strProp("The domain's id."),
			"record_id": strProp("The record's id, from get_store_domain."),
		}, "domain_id", "record_id"),
		Handler: func(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
			id, err := idFrom(args, "domain_id")
			if err != nil {
				return "", err
			}
			rid, err := idFrom(args, "record_id")
			if err != nil {
				return "", err
			}
			if err := s.DeleteDNSRecord(ctx, inv.OrgID, id, rid); err != nil {
				return "", storeToolErr(err)
			}
			d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityStoreDomain, &id, map[string]string{"dns_record_id": rid.String()})
			return jsonResult(map[string]any{"deleted": true})
		},
	})
}

type storePageArgs struct {
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

// idFrom reads one required UUID argument by name.
func idFrom(args json.RawMessage, name string) (uuid.UUID, error) {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return uuid.Nil, ErrInvalidArgs
	}
	s, _ := m[name].(string)
	return parseUUIDArg(s)
}

func dollars(micros int64) string { return fmt.Sprintf("%.2f", float64(micros)/1_000_000) }

func storeResult[T any](v *T, err error) (string, error) {
	if err != nil {
		return "", storeToolErr(err)
	}
	return jsonResult(v)
}

// storeToolErr is the store's refusal in words the assistant can act on; anything unexpected stays in the log.
func storeToolErr(err error) error {
	var (
		price  *store.PriceChangedError
		method *store.PaymentMethodError
		card   *store.CardError
		bad    *store.ContactError
		xerr   *errx.Error
	)
	switch {
	case errors.As(err, &price) && price.Quote != nil:
		return fmt.Errorf("price_changed: the total is now $%s (quote %s). Nothing was charged; confirm the new total with the person before ordering again", dollars(price.Quote.DueNowMicros), price.Quote.ID)
	case errors.As(err, &method):
		return errors.New("payment_method_required: the balance is short and no card is saved. Add a card or money in Settings > Billing, then try again")
	case errors.As(err, &card):
		return errors.New(card.Code + ": the card was not charged. Try another card or add money to the balance")
	case errors.As(err, &bad):
		return errors.New("contact_invalid: " + bad.Message())
	case errors.As(err, &xerr):
		if code := xerr.ResponseCode(); code != "" {
			return errors.New(code + ": " + xerr.Message)
		}
		return errors.New(xerr.Message)
	}
	log.Error().Err(err).Msg("store tool: request failed")
	return errors.New("the request failed; try again in a moment")
}
