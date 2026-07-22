package aitools

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func (d Deps) registerContactTools(r *Registry) {
	r.Register(Tool{
		Name:        "search_contacts",
		Description: "Search the organization's contacts by a text query (matches name, email, company). Returns a page of matching contacts with their ids.",
		InputSchema: objectSchema(map[string]any{
			"query": strProp("Text to search for across name, email, and company. Empty returns recent contacts."),
			"limit": intProp("Max contacts to return (1-50, default 20)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.searchContacts,
	})

	r.Register(Tool{
		Name:        "get_contact",
		Description: "Get one contact by id, including custom fields, categories, subscription state, and engagement summary.",
		InputSchema: objectSchema(map[string]any{
			"contact_id": strProp("The contact's UUID."),
		}, "contact_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.getContact,
	})

	r.Register(Tool{
		Name:        "update_contact_fields",
		Description: "Update editable fields on a contact (name, company, phone, custom fields, subscription). Only provided fields change.",
		InputSchema: objectSchema(map[string]any{
			"contact_id":    strProp("The contact's UUID."),
			"first_name":    strProp("New first name."),
			"last_name":     strProp("New last name."),
			"company":       strProp("New company."),
			"phone":         strProp("New phone."),
			"custom_fields": objProp("Custom field key/value updates (merged into existing)."),
			"subscribed":    boolProp("Subscription state."),
		}, "contact_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.updateContactFields,
	})

	r.Register(Tool{
		Name:        "add_tag",
		Description: "Add a category (tag) to a contact. The category_id comes from a contact's categories in get_contact/search results.",
		InputSchema: objectSchema(map[string]any{
			"contact_id":  strProp("The contact's UUID."),
			"category_id": strProp("The category (tag) UUID to add."),
		}, "contact_id", "category_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.addTag,
	})

	r.Register(Tool{
		Name:        "remove_tag",
		Description: "Remove a category (tag) from a contact.",
		InputSchema: objectSchema(map[string]any{
			"contact_id":  strProp("The contact's UUID."),
			"category_id": strProp("The category (tag) UUID to remove."),
		}, "contact_id", "category_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.removeTag,
	})

	r.Register(Tool{
		Name:        "add_contact",
		Description: "Create a new contact in the organization. Email is required. Returns the new contact's id.",
		InputSchema: objectSchema(map[string]any{
			"first_name":    strProp("First name."),
			"last_name":     strProp("Last name."),
			"email":         strProp("Email address (required, must be unique in the org)."),
			"company":       strProp("Company."),
			"phone":         strProp("Phone."),
			"custom_fields": objProp("Custom field key/value pairs."),
			"categories":    arrProp("Category (tag) UUIDs to attach.", strProp("Category UUID.")),
		}, "email"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.addContact,
	})

	r.Register(Tool{
		Name:        "delete_contact",
		Description: "Permanently delete a contact. Destructive and not reversible; requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"contact_id": strProp("The contact's UUID."),
		}, "contact_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.deleteContact,
	})

	r.Register(Tool{
		Name:        "bulk_edit_contacts",
		Description: "Apply the same change (add/remove tags, set subscription) to many contacts at once.",
		InputSchema: objectSchema(map[string]any{
			"contact_ids":       arrProp("Contact UUIDs to edit (required).", strProp("Contact UUID.")),
			"add_categories":    arrProp("Category (tag) UUIDs to add to each contact.", strProp("Category UUID.")),
			"remove_categories": arrProp("Category (tag) UUIDs to remove from each contact.", strProp("Category UUID.")),
			"subscribe":         boolProp("Set subscription state on each contact."),
		}, "contact_ids"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermBulkContacts,
		Handler:         d.bulkEditContacts,
	})

	r.Register(Tool{
		Name:        "get_contact_timeline",
		Description: "Get a contact's merged activity timeline (opens, clicks, replies, notes, CRM events), newest first.",
		InputSchema: objectSchema(map[string]any{
			"contact_id": strProp("The contact's UUID."),
			"limit":      intProp("Max events (1-100, default 50)."),
		}, "contact_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.getContactTimeline,
	})

	r.Register(Tool{
		Name:        "get_contact_sent_emails",
		Description: "List the emails we have sent (or attempted to send) to a contact, newest first.",
		InputSchema: objectSchema(map[string]any{
			"contact_id": strProp("The contact's UUID."),
			"limit":      intProp("Max emails (1-100, default 50)."),
		}, "contact_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.getContactSentEmails,
	})
}

func (d Deps) addContact(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		FirstName    string            `json:"first_name"`
		LastName     string            `json:"last_name"`
		Email        string            `json:"email"`
		Company      string            `json:"company"`
		Phone        string            `json:"phone"`
		CustomFields map[string]string `json:"custom_fields"`
		Categories   []string          `json:"categories"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Email == "" {
		return "", ErrInvalidArgs
	}
	created, xerr := d.Contacts.Add(ctx, inv.UserID.String(), inv.OrgID, []models.AddContact{{
		FirstName:    in.FirstName,
		LastName:     in.LastName,
		Email:        in.Email,
		Company:      in.Company,
		Phone:        in.Phone,
		Categories:   in.Categories,
		CustomFields: in.CustomFields,
	}})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	if len(created) == 0 {
		return "", ErrInvalidArgs
	}
	cid := created[0].ID
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityContact, &cid, map[string]string{"email": in.Email})
	return jsonResult(map[string]any{"ok": true, "contact_id": cid.String()})
}

func (d Deps) deleteContact(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID string `json:"contact_id"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	if xerr := d.Contacts.Delete(ctx, inv.UserID.String(), inv.OrgID, cid.String()); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityContact, &cid, nil)
	return jsonResult(map[string]any{"ok": true, "contact_id": cid.String()})
}

func (d Deps) bulkEditContacts(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactIDs       []string `json:"contact_ids"`
		AddCategories    []string `json:"add_categories"`
		RemoveCategories []string `json:"remove_categories"`
		Subscribe        *bool    `json:"subscribe"`
	}](args)
	if err != nil {
		return "", err
	}
	if len(in.ContactIDs) == 0 {
		return "", ErrInvalidArgs
	}
	data := &models.BulkEditContactsData{
		Contacts:         in.ContactIDs,
		AddCategories:    in.AddCategories,
		RemoveCategories: in.RemoveCategories,
		Subscribe:        in.Subscribe,
	}
	updated, xerr := d.Contacts.BulkUpdate(ctx, inv.UserID.String(), inv.OrgID, data)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityContact, nil, map[string]string{"count": strconv.Itoa(len(updated))})
	return jsonResult(map[string]any{"ok": true, "updated": len(updated)})
}

func (d Deps) getContactTimeline(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID string `json:"contact_id"`
		Limit     int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	orgID := inv.OrgID
	res, xerr := d.Contacts.ListTimeline(ctx, inv.UserID, &orgID, cid, limit, nil)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(res)
}

func (d Deps) getContactSentEmails(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID string `json:"contact_id"`
		Limit     int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	res, xerr := d.Contacts.ListSentEmails(ctx, inv.UserID, cid, limit, nil, nil)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(res)
}

type compactContact struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Company string `json:"company,omitempty"`
}

func (d Deps) searchContacts(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	res, xerr := d.Contacts.Search(ctx, inv.OrgID.String(), "", "", strconv.Itoa(limit), models.SearchContacts{Query: in.Query})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]compactContact, 0, len(res.Data))
	for _, c := range res.Data {
		out = append(out, compactContact{
			ID:      c.ID.String(),
			Name:    fullName(c.FirstName, c.LastName),
			Email:   c.Email,
			Company: c.Company,
		})
	}
	return jsonResult(map[string]any{"contacts": out, "count": len(out)})
}

func (d Deps) getContact(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID string `json:"contact_id"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	orgID := inv.OrgID
	detail, xerr := d.Contacts.GetDetail(ctx, inv.UserID, &orgID, cid)
	if xerr != nil {
		return "", fromErrx(xerr)
	}

	cats := make([]map[string]string, 0, len(detail.Categories))
	for _, c := range detail.Categories {
		cats = append(cats, map[string]string{"id": c.ID.String(), "name": c.Title})
	}
	return jsonResult(map[string]any{
		"id":            detail.ID.String(),
		"name":          fullName(detail.FirstName, detail.LastName),
		"email":         detail.Email,
		"company":       detail.Company,
		"phone":         detail.Phone,
		"subscribed":    detail.Subscribed,
		"custom_fields": detail.CustomFields,
		"categories":    cats,
		"engagement": map[string]any{
			"sent":    detail.Engagement.TotalSent,
			"opened":  detail.Engagement.TotalOpened,
			"clicked": detail.Engagement.TotalClicked,
			"replied": detail.Engagement.TotalReplied,
		},
	})
}

func (d Deps) updateContactFields(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		ContactID    string             `json:"contact_id"`
		FirstName    *string            `json:"first_name"`
		LastName     *string            `json:"last_name"`
		Company      *string            `json:"company"`
		Phone        *string            `json:"phone"`
		CustomFields *map[string]string `json:"custom_fields"`
		Subscribed   *bool              `json:"subscribed"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}

	upd := &models.UpdateContact{
		FirstName:    in.FirstName,
		LastName:     in.LastName,
		Company:      in.Company,
		Phone:        in.Phone,
		CustomFields: in.CustomFields,
		Subscribed:   in.Subscribed,
	}
	if _, xerr := d.Contacts.Update(ctx, inv.UserID.String(), cid.String(), inv.OrgID, upd); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityContact, &cid, nil)
	return jsonResult(map[string]any{"ok": true, "contact_id": cid.String()})
}

func (d Deps) addTag(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	return d.tagOp(ctx, inv, args, true)
}

func (d Deps) removeTag(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	return d.tagOp(ctx, inv, args, false)
}

// tagOp adds or removes a category on a contact via the contact update path
// (categories are the contact tag system). add=false removes.
func (d Deps) tagOp(ctx context.Context, inv Invocation, args json.RawMessage, add bool) (string, error) {
	in, err := decodeArgs[struct {
		ContactID  string `json:"contact_id"`
		CategoryID string `json:"category_id"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	if _, err := parseUUIDArg(in.CategoryID); err != nil {
		return "", err
	}

	upd := &models.UpdateContact{}
	if add {
		upd.AddCategories = []string{in.CategoryID}
	} else {
		upd.RemoveCategories = []string{in.CategoryID}
	}
	if _, xerr := d.Contacts.Update(ctx, inv.UserID.String(), cid.String(), inv.OrgID, upd); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityContact, &cid, nil)
	return jsonResult(map[string]any{"ok": true, "contact_id": cid.String()})
}

func fullName(first, last string) string {
	if first == "" && last == "" {
		return ""
	}
	if first == "" {
		return last
	}
	if last == "" {
		return first
	}
	return first + " " + last
}
