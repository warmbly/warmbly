package aitools

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Hosted form tools, gated on the contact permissions like the HTTP routes.
// Brand-asset uploads (multipart images) and the DNS-checked forms domain are
// deliberately absent.
func (d Deps) registerFormTools(r *Registry) {
	if d.Forms == nil {
		return
	}

	// One block. The layout kinds render but collect nothing.
	fieldItem := objectSchema(map[string]any{
		"id":          strProp("Short lowercase slug, stable across edits; answers are keyed by it."),
		"type":        enumProp("The block kind.", "text", "email", "phone", "textarea", "number", "select", "radio", "checkboxes", "checkbox", "date", "hidden", "heading", "paragraph", "divider", "page_break"),
		"label":       strProp("The visible label. Required for every input block except hidden."),
		"placeholder": strProp("Optional placeholder text."),
		"help_text":   strProp("Optional hint under the field."),
		"required":    boolProp("Whether an answer is required to submit."),
		"options":     arrProp("The choices, for select, radio and checkboxes.", map[string]any{"type": "string"}),
		"map_to":      enumProp("Fill this contact column with the answer. Without it the answer lands in the contact's custom fields under the label.", "first_name", "last_name", "email", "company", "phone"),
		"value":       strProp("The constant a hidden field submits, or the body text of a paragraph block."),
		"width":       enumProp("full, or half so two fields share a row.", "full", "half"),
	}, "id", "type")

	r.Register(Tool{
		Name:            "list_forms",
		Description:     "List the workspace's hosted forms with their status, public URL, and view/submission counts.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.listForms,
	})

	r.Register(Tool{
		Name:            "get_form",
		Description:     "Get one form: its blocks, design, target campaign, categories, and public URL.",
		InputSchema:     objectSchema(map[string]any{"form_id": strProp("The form's UUID.")}, "form_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.getForm,
	})

	r.Register(Tool{
		Name:            "create_form",
		Description:     "Create a form. It starts as an unpublished draft with no blocks; add them with update_form, then publish it by setting status to published.",
		InputSchema:     objectSchema(map[string]any{"name": strProp("Internal name for the form (required).")}, "name"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.createForm,
	})

	r.Register(Tool{
		Name:        "update_form",
		Description: "Update a form. Omitted fields keep their stored value; sending fields replaces the whole block list. Only a published form renders and accepts submissions.",
		InputSchema: objectSchema(map[string]any{
			"form_id":         strProp("The form's UUID."),
			"name":            strProp("New internal name."),
			"status":          enumProp("draft hides it, published puts it live, archived keeps the data but takes the page offline.", "draft", "published", "archived"),
			"fields":          arrProp("Replacement block list, in display order.", fieldItem),
			"success_message": strProp("Shown after a successful submit."),
			"redirect_url":    strProp("Send the visitor here instead of showing the success message."),
			"campaign_id":     strProp("Add every contact this form creates to this campaign. Pass an empty string to detach."),
			"category_ids":    arrProp("Categories to file new contacts under.", map[string]any{"type": "string"}),
			"allowed_domains": arrProp("Domains the form may be embedded on.", map[string]any{"type": "string"}),
			"captcha_enabled": boolProp("Require a captcha. Has no effect where the install has no captcha provider."),
			"triage_enabled":  boolProp("Classify each submission as buyer, vendor, job seeker, other or junk; junk creates no contact. Has no effect where the install has no TypeSafe key."),
		}, "form_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.updateForm,
	})

	r.Register(Tool{
		Name:            "delete_form",
		Description:     "Delete a form and every submission it captured. The contacts it created are not touched. Requires user approval.",
		InputSchema:     objectSchema(map[string]any{"form_id": strProp("The form's UUID.")}, "form_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.deleteForm,
	})

	r.Register(Tool{
		Name:        "list_form_submissions",
		Description: "List a form's submissions, newest first, with the contact each one created or updated.",
		InputSchema: objectSchema(map[string]any{
			"form_id": strProp("The form's UUID."),
			"limit":   intProp("Max submissions (1-100, default 50)."),
		}, "form_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.listFormSubmissions,
	})

	r.Register(Tool{
		Name:        "get_form_stats",
		Description: "Get a form's funnel over a date range: views, starts, submissions, completion rate, per-page drop-off, and breakdowns by source, country, device and campaign.",
		InputSchema: objectSchema(map[string]any{
			"form_id": strProp("The form's UUID."),
			"range":   enumProp("The window to report on. Defaults to 30d.", "7d", "30d", "90d"),
		}, "form_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.getFormStats,
	})

	r.Register(Tool{
		Name:            "get_campaign_forms",
		Description:     "Report how the forms linked from a campaign performed for that campaign's recipients: links sent, viewers, starters, submissions.",
		InputSchema:     objectSchema(map[string]any{"campaign_id": strProp("The campaign's UUID.")}, "campaign_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler:         d.getCampaignForms,
	})

	r.Register(Tool{
		Name:        "mint_form_link",
		Description: "Get the personalized form URL for one contact, so a submission is tied back to them. Minting the same link twice returns the same URL.",
		InputSchema: objectSchema(map[string]any{
			"form_id":    strProp("The form's UUID."),
			"contact_id": strProp("The contact's UUID."),
		}, "form_id", "contact_id"),
		// Idempotent, so it stays out of the approval loop, but it upserts a
		// ticket row, hence the write permission. Same shape as
		// get_invitation_link.
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.mintFormLink,
	})
}

func (d Deps) listForms(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	out, xerr := d.Forms.List(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(out)
}

func (d Deps) getForm(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		FormID string `json:"form_id"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.FormID)
	if err != nil {
		return "", err
	}
	out, xerr := d.Forms.Get(ctx, inv.OrgID, id)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(out)
}

func (d Deps) createForm(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name string `json:"name"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Name == "" {
		return "", ErrInvalidArgs
	}
	createdBy := inv.UserID
	out, xerr := d.Forms.Create(ctx, inv.OrgID, &createdBy, in.Name)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntityForm, &out.ID, map[string]string{"name": out.Name})
	return jsonResult(out)
}

func (d Deps) updateForm(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		FormID         string           `json:"form_id"`
		Name           *string          `json:"name"`
		Status         string           `json:"status"`
		Fields         *[]toolFormField `json:"fields"`
		SuccessMessage *string          `json:"success_message"`
		RedirectURL    *string          `json:"redirect_url"`
		CampaignID     *string          `json:"campaign_id"`
		CategoryIDs    *[]string        `json:"category_ids"`
		AllowedDomains *[]string        `json:"allowed_domains"`
		CaptchaEnabled *bool            `json:"captcha_enabled"`
		TriageEnabled  *bool            `json:"triage_enabled"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.FormID)
	if err != nil {
		return "", err
	}

	write := &models.FormWrite{
		Name:           in.Name,
		SuccessMessage: in.SuccessMessage,
		RedirectURL:    in.RedirectURL,
		AllowedDomains: in.AllowedDomains,
		CaptchaEnabled: in.CaptchaEnabled,
		TriageEnabled:  in.TriageEnabled,
	}
	if in.Status != "" {
		status := models.FormStatus(in.Status)
		if !status.Valid() {
			return "", ErrInvalidArgs
		}
		write.Status = &status
	}
	if in.Fields != nil {
		fields, ferr := toFormFields(*in.Fields)
		if ferr != nil {
			return "", ferr
		}
		write.Fields = &fields
	}
	// "" detaches; the nullable wrapper tells "clear it" from "leave it".
	if in.CampaignID != nil {
		if *in.CampaignID == "" {
			write.CampaignID = models.NullableUUID{Set: true}
		} else {
			cid, cerr := parseUUIDArg(*in.CampaignID)
			if cerr != nil {
				return "", cerr
			}
			write.CampaignID = models.NullableUUID{Set: true, Value: &cid}
		}
	}
	if in.CategoryIDs != nil {
		ids := make([]uuid.UUID, 0, len(*in.CategoryIDs))
		for _, raw := range *in.CategoryIDs {
			cid, cerr := parseUUIDArg(raw)
			if cerr != nil {
				return "", cerr
			}
			ids = append(ids, cid)
		}
		write.CategoryIDs = &ids
	}

	out, xerr := d.Forms.Update(ctx, inv.OrgID, id, write)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityForm, &out.ID, map[string]string{
		"name": out.Name, "status": string(out.Status),
	})
	return jsonResult(out)
}

func (d Deps) deleteForm(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		FormID string `json:"form_id"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.FormID)
	if err != nil {
		return "", err
	}
	if xerr := d.Forms.Delete(ctx, inv.OrgID, id); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntityForm, &id, nil)
	return jsonResult(map[string]any{"ok": true, "form_id": id.String()})
}

func (d Deps) listFormSubmissions(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		FormID string `json:"form_id"`
		Limit  int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.FormID)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	out, hasMore, xerr := d.Forms.ListSubmissions(ctx, inv.OrgID, id, limit, nil)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"data": out, "has_more": hasMore})
}

func (d Deps) getFormStats(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		FormID string `json:"form_id"`
		Range  string `json:"range"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.FormID)
	if err != nil {
		return "", err
	}
	days := 30
	switch in.Range {
	case "", "30d":
	case "7d":
		days = 7
	case "90d":
		days = 90
	default:
		return "", ErrInvalidArgs
	}
	out, xerr := d.Forms.Stats(ctx, inv.OrgID, id, days)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(out)
}

func (d Deps) getCampaignForms(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.CampaignID)
	if err != nil {
		return "", err
	}
	out, xerr := d.Forms.CampaignForms(ctx, inv.OrgID, cid)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(out)
}

func (d Deps) mintFormLink(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		FormID    string `json:"form_id"`
		ContactID string `json:"contact_id"`
	}](args)
	if err != nil {
		return "", err
	}
	fid, err := parseUUIDArg(in.FormID)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.ContactID)
	if err != nil {
		return "", err
	}
	url, xerr := d.Forms.MintLink(ctx, inv.OrgID, fid, cid)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"url": url})
}

// Model-facing block shape, kept off the stored struct's json tags.
type toolFormField struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Placeholder string   `json:"placeholder"`
	HelpText    string   `json:"help_text"`
	Required    bool     `json:"required"`
	Options     []string `json:"options"`
	MapTo       string   `json:"map_to"`
	Value       string   `json:"value"`
	Width       string   `json:"width"`
	Rows        int      `json:"rows"`
}

// The service does the real validation; this only rejects an unknown block
// type, so a bad one is a correctable argument error, not a round-trip.
func toFormFields(in []toolFormField) ([]models.FormField, error) {
	out := make([]models.FormField, 0, len(in))
	for _, f := range in {
		ft := models.FormFieldType(f.Type)
		if !ft.Valid() {
			return nil, ErrInvalidArgs
		}
		out = append(out, models.FormField{
			ID:          f.ID,
			Type:        ft,
			Label:       f.Label,
			Placeholder: f.Placeholder,
			HelpText:    f.HelpText,
			Required:    f.Required,
			Options:     f.Options,
			MapTo:       f.MapTo,
			Value:       f.Value,
			Width:       f.Width,
			Rows:        f.Rows,
		})
	}
	return out, nil
}
