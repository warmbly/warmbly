package aitools

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// Segment tools, gated on the contact permissions like the HTTP routes, except
// the two campaign-linking ones: attaching an audience is a campaign write.
func (d Deps) registerSegmentTools(r *Registry) {
	if d.Segments == nil {
		return
	}

	// Shared by create, update and preview.
	conditionItem := objectSchema(map[string]any{
		"field":    strProp("Field name from list_segment_fields, or \"custom.<key>\" for a custom field."),
		"operator": strProp("An operator that field's kind accepts, from list_segment_fields."),
		"value":    strProp("The value, for scalar operators. A date field takes YYYY-MM-DD; within_days takes a whole number of days."),
		"values":   arrProp("The values, for the list operators in / not_in.", map[string]any{"type": "string"}),
	}, "field", "operator")

	r.Register(Tool{
		Name:            "list_segments",
		Description:     "List the workspace's saved contact segments with their live membership counts.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.listSegments,
	})

	r.Register(Tool{
		Name:            "get_segment",
		Description:     "Get one segment: its conditions, match mode, and how many contacts it currently holds.",
		InputSchema:     objectSchema(map[string]any{"segment_id": strProp("The segment's UUID.")}, "segment_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.getSegment,
	})

	r.Register(Tool{
		Name:            "list_segment_fields",
		Description:     "List every field a segment condition can name, with its kind and the operators it accepts. Call this before building conditions: the vocabulary includes this workspace's own custom fields and cannot be guessed.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.listSegmentFields,
	})

	r.Register(Tool{
		Name:        "preview_segment",
		Description: "Count the contacts a set of conditions would match, without saving anything. Use it to check an audience is the size you expect before creating the segment.",
		InputSchema: objectSchema(map[string]any{
			"match":      enumProp("Whether every condition must hold, or any one of them. Defaults to all.", "all", "any"),
			"conditions": arrProp("The conditions to evaluate.", conditionItem),
			"segment_id": strProp("An existing segment whose manual overrides should be kept in the count, when previewing an edit to it."),
		}, "conditions"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewContacts,
		RequiredAPIPerm: models.APIPermReadContacts,
		Handler:         d.previewSegment,
	})

	r.Register(Tool{
		Name:        "create_segment",
		Description: "Create a saved contact segment. Membership is evaluated live, so the segment keeps up with the contacts on its own.",
		InputSchema: objectSchema(map[string]any{
			"name":        strProp("A short name for the audience (required)."),
			"description": strProp("Optional longer description."),
			"color":       strProp("Optional #rrggbb swatch for the dashboard."),
			"match":       enumProp("Whether every condition must hold, or any one of them. Defaults to all.", "all", "any"),
			"conditions":  arrProp("The membership conditions.", conditionItem),
		}, "name", "conditions"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.createSegment,
	})

	r.Register(Tool{
		Name:        "update_segment",
		Description: "Update a segment's name, description, colour, match mode, or conditions. Omitted fields keep their stored value; sending conditions replaces the whole list.",
		InputSchema: objectSchema(map[string]any{
			"segment_id":  strProp("The segment's UUID."),
			"name":        strProp("New name."),
			"description": strProp("New description."),
			"color":       strProp("New #rrggbb swatch."),
			"match":       enumProp("New match mode.", "all", "any"),
			"conditions":  arrProp("Replacement condition list.", conditionItem),
		}, "segment_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.updateSegment,
	})

	r.Register(Tool{
		Name:            "delete_segment",
		Description:     "Delete a segment. The contacts in it are not touched, but any campaign fed by it stops receiving new members. Requires user approval.",
		InputSchema:     objectSchema(map[string]any{"segment_id": strProp("The segment's UUID.")}, "segment_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.deleteSegment,
	})

	r.Register(Tool{
		Name:        "set_segment_members",
		Description: "Pin contacts into or out of a segment regardless of what its conditions say, or clear that override so the conditions decide again.",
		InputSchema: objectSchema(map[string]any{
			"segment_id": strProp("The segment's UUID."),
			"contacts":   arrProp("Contact UUIDs to override.", map[string]any{"type": "string"}),
			"mode":       enumProp("include pins them in, exclude pins them out, auto clears the override.", "include", "exclude", "auto"),
		}, "segment_id", "contacts", "mode"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageContacts,
		RequiredAPIPerm: models.APIPermWriteContacts,
		Handler:         d.setSegmentMembers,
	})

	r.Register(Tool{
		Name:            "list_campaign_segments",
		Description:     "List the segments feeding a campaign, with how many of each one's members are already leads and how many are held out.",
		InputSchema:     objectSchema(map[string]any{"campaign_id": strProp("The campaign's UUID.")}, "campaign_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewCampaigns,
		RequiredAPIPerm: models.APIPermReadCampaigns,
		Handler:         d.listCampaignSegments,
	})

	r.Register(Tool{
		Name:        "set_campaign_segments",
		Description: "Replace the set of segments feeding a campaign. A linked segment is a live audience source: its members are enrolled as leads now and kept enrolled as the segment changes. This is the whole set, so pass every segment you want attached, or an empty list to detach them all. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("The campaign's UUID."),
			"segment_ids": arrProp("The segment UUIDs to link, at most 20. An empty list detaches every segment.", map[string]any{"type": "string"}),
		}, "campaign_id", "segment_ids"),
		Risk: generation.RiskWrite,
		// Linking an audience changes who a campaign mails.
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.setCampaignSegments,
	})

	r.Register(Tool{
		Name:        "add_segment_to_campaign",
		Description: "Enrol a segment's current members as leads of a campaign, once. This is a one-off copy: later members are NOT added. Use set_campaign_segments instead to keep the audience live. Requires user approval.",
		InputSchema: objectSchema(map[string]any{
			"segment_id":  strProp("The segment's UUID."),
			"campaign_id": strProp("The campaign to enrol them into."),
		}, "segment_id", "campaign_id"),
		Risk:            generation.RiskWrite,
		RequiredOrgPerm: models.PermManageCampaigns,
		RequiredAPIPerm: models.APIPermWriteCampaigns,
		Handler:         d.addSegmentToCampaign,
	})
}

// Model-facing condition shape, kept off the stored struct's json tags.
type toolCondition struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Value    string   `json:"value"`
	Values   []string `json:"values"`
}

func toSegmentConditions(in []toolCondition) []models.SegmentCondition {
	out := make([]models.SegmentCondition, 0, len(in))
	for _, c := range in {
		out = append(out, models.SegmentCondition{
			Field:    c.Field,
			Operator: c.Operator,
			Value:    c.Value,
			Values:   c.Values,
		})
	}
	return out
}

// Defaults to "all"; an unknown mode is rejected, since "any" can differ by the
// whole audience.
func segmentMatch(s string) (models.SegmentMatch, error) {
	switch s {
	case "":
		return models.SegmentMatchAll, nil
	case string(models.SegmentMatchAll), string(models.SegmentMatchAny):
		return models.SegmentMatch(s), nil
	default:
		return "", ErrInvalidArgs
	}
}

func (d Deps) listSegments(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	out, xerr := d.Segments.List(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(out)
}

func (d Deps) getSegment(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		SegmentID string `json:"segment_id"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.SegmentID)
	if err != nil {
		return "", err
	}
	out, xerr := d.Segments.Get(ctx, inv.OrgID, id)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(out)
}

func (d Deps) listSegmentFields(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	out, xerr := d.Segments.Fields(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(out)
}

func (d Deps) previewSegment(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Match      string          `json:"match"`
		Conditions []toolCondition `json:"conditions"`
		SegmentID  string          `json:"segment_id"`
	}](args)
	if err != nil {
		return "", err
	}
	match, err := segmentMatch(in.Match)
	if err != nil {
		return "", err
	}
	req := &models.SegmentPreview{Match: match, Conditions: toSegmentConditions(in.Conditions)}
	if in.SegmentID != "" {
		id, perr := parseUUIDArg(in.SegmentID)
		if perr != nil {
			return "", perr
		}
		req.ID = &id
	}
	n, xerr := d.Segments.Preview(ctx, inv.OrgID, req)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(map[string]any{"contact_count": n})
}

func (d Deps) createSegment(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name        string          `json:"name"`
		Description *string         `json:"description"`
		Color       *string         `json:"color"`
		Match       string          `json:"match"`
		Conditions  []toolCondition `json:"conditions"`
	}](args)
	if err != nil {
		return "", err
	}
	if in.Name == "" {
		return "", ErrInvalidArgs
	}
	match, err := segmentMatch(in.Match)
	if err != nil {
		return "", err
	}
	conds := toSegmentConditions(in.Conditions)
	createdBy := inv.UserID
	out, xerr := d.Segments.Create(ctx, inv.OrgID, &createdBy, &models.SegmentWrite{
		Name:        &in.Name,
		Description: in.Description,
		Color:       in.Color,
		Match:       &match,
		Conditions:  &conds,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionCreate, models.AuditEntitySegment, &out.ID, map[string]string{"name": out.Name})
	return jsonResult(out)
}

func (d Deps) updateSegment(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		SegmentID   string           `json:"segment_id"`
		Name        *string          `json:"name"`
		Description *string          `json:"description"`
		Color       *string          `json:"color"`
		Match       string           `json:"match"`
		Conditions  *[]toolCondition `json:"conditions"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.SegmentID)
	if err != nil {
		return "", err
	}
	write := &models.SegmentWrite{Name: in.Name, Description: in.Description, Color: in.Color}
	if in.Match != "" {
		match, merr := segmentMatch(in.Match)
		if merr != nil {
			return "", merr
		}
		write.Match = &match
	}
	if in.Conditions != nil {
		conds := toSegmentConditions(*in.Conditions)
		write.Conditions = &conds
	}
	out, xerr := d.Segments.Update(ctx, inv.OrgID, id, write)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntitySegment, &out.ID, map[string]string{"name": out.Name})
	return jsonResult(out)
}

func (d Deps) deleteSegment(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		SegmentID string `json:"segment_id"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.SegmentID)
	if err != nil {
		return "", err
	}
	if xerr := d.Segments.Delete(ctx, inv.OrgID, id); xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionDelete, models.AuditEntitySegment, &id, nil)
	return jsonResult(map[string]any{"ok": true, "segment_id": id.String()})
}

func (d Deps) setSegmentMembers(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		SegmentID string   `json:"segment_id"`
		Contacts  []string `json:"contacts"`
		Mode      string   `json:"mode"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.SegmentID)
	if err != nil {
		return "", err
	}
	mode := models.SegmentMemberMode(in.Mode)
	switch mode {
	case models.SegmentMemberInclude, models.SegmentMemberExclude, models.SegmentMemberAuto:
	default:
		return "", ErrInvalidArgs
	}
	if len(in.Contacts) == 0 {
		return "", ErrInvalidArgs
	}
	n, xerr := d.Segments.SetMembers(ctx, inv.OrgID, id, &models.SegmentMembersWrite{
		ContactSelection: models.ContactSelection{Contacts: in.Contacts},
		Mode:             mode,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntitySegment, &id, map[string]string{"members": in.Mode})
	return jsonResult(map[string]any{"updated": n})
}

func (d Deps) listCampaignSegments(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
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
	out, xerr := d.Segments.ListCampaignSegments(ctx, inv.OrgID, cid)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(out)
}

func (d Deps) setCampaignSegments(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	// Pointer so an omitted key is told apart from an explicit [], which means
	// detach everything.
	in, err := decodeArgs[struct {
		CampaignID string    `json:"campaign_id"`
		SegmentIDs *[]string `json:"segment_ids"`
	}](args)
	if err != nil {
		return "", err
	}
	cid, err := parseUUIDArg(in.CampaignID)
	if err != nil {
		return "", err
	}
	if in.SegmentIDs == nil {
		return "", ErrInvalidArgs
	}
	links, added, change, xerr := d.Segments.SetCampaignSegments(ctx, inv.OrgID, cid, &models.CampaignSegmentsWrite{
		SegmentIDs: *in.SegmentIDs,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCampaign, &cid, map[string]string{
		"segments": strconv.Itoa(len(links)), "added": strconv.Itoa(added),
		"withdrawn": strconv.Itoa(change.Withdrawn),
	})
	return jsonResult(map[string]any{"data": links, "added": added, "withdrawn": change.Withdrawn, "contacted": change.Contacted})
}

func (d Deps) addSegmentToCampaign(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		SegmentID  string `json:"segment_id"`
		CampaignID string `json:"campaign_id"`
	}](args)
	if err != nil {
		return "", err
	}
	sid, err := parseUUIDArg(in.SegmentID)
	if err != nil {
		return "", err
	}
	if _, perr := parseUUIDArg(in.CampaignID); perr != nil {
		return "", perr
	}
	res, xerr := d.Segments.AddToCampaign(ctx, inv.OrgID, inv.UserID.String(), sid, &models.SegmentAddToCampaign{
		CampaignID: in.CampaignID,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	cid := res.CampaignID
	d.logAudit(ctx, inv, models.AuditActionUpdate, models.AuditEntityCampaign, &cid, map[string]string{
		"segment_id": sid.String(), "added": strconv.Itoa(res.Added),
	})
	return jsonResult(res)
}
