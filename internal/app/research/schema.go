package research

// saveResearchSchema is the JSON Schema for the save_research tool input,
// mirroring models.ResearchResult. Server-side validation (validateResearch) is
// the real enforcement; this shapes what the model produces.
func saveResearchSchema() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	strArr := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}

	artifact := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"what":  str("What the artifact is (a post, talk, article, profile)."),
			"where": str("Where it lives (LinkedIn, a blog, a news site)."),
			"when":  str("Rough date if known."),
			"url":   str("The url you fetched. Required."),
		},
		"required": []string{"what", "url"},
	}
	signal := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"type":       str("Kind of signal (hire, funding, launch, pricing, post, etc.)."),
			"fact":       str("The specific, verifiable fact."),
			"when":       str("Rough date if known."),
			"url":        str("The url you fetched. Required."),
			"confidence": map[string]any{"type": "string", "enum": []any{"high", "medium", "low"}},
		},
		"required": []string{"type", "fact", "url", "confidence"},
	}
	hook := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"based_on":     str("Which signal this opener is built on."),
			"why_relevant": str("Why it matters to what the customer sells."),
			"opener_line":  str("One plain sentence a human could send. No em dashes."),
		},
		"required": []string{"based_on", "opener_line"},
	}

	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"company": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"summary":               str("One or two sentences on what the company does."),
					"industry":              str("Industry."),
					"size_estimate":         str("Rough headcount or size band."),
					"sells_to":              str("Who the company sells to."),
					"tech_or_stack_signals": strArr,
				},
			},
			"person": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"role_confirmed":   map[string]any{"type": "boolean"},
					"title":            str("Verified title, if confirmed."),
					"public_artifacts": map[string]any{"type": "array", "items": artifact},
				},
			},
			"signals": map[string]any{"type": "array", "maxItems": 5, "items": signal},
			"hooks":   map[string]any{"type": "array", "maxItems": 3, "items": hook},
			"custom_field_updates": map[string]any{
				"type":                 "object",
				"description":          "Only fields you are confident about.",
				"additionalProperties": map[string]any{"type": "string"},
			},
			"research_notes": str("A short honest note on what you found and could not."),
			"nothing_found":  map[string]any{"type": "boolean", "description": "True only when you have no cited signal."},
		},
		"required": []string{"nothing_found"},
	}
}
