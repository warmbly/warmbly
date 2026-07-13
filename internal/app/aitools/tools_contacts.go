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
