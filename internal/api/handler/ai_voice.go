package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// orgVoice loads the org's voice profile (product description, ICP notes, house
// style) and folds in the caller's tone, so every AI writing surface (writing
// assistant, reply drafts) is grounded the same way through
// generation.BuildVoiceRules. On any load failure it returns just the tone; the
// built-in humanizer rules still apply.
func (h *Handler) orgVoice(ctx context.Context, orgID uuid.UUID, tone string) generation.VoiceContext {
	vc := generation.VoiceContext{Tone: tone}
	if h.OrganizationService == nil {
		return vc
	}
	if org, err := h.OrganizationService.Get(ctx, orgID); err == nil && org != nil {
		vc.ProductDescription = org.ProductDescription
		vc.ICPNotes = org.ICPNotes
		vc.VoiceProfile = org.VoiceProfile
	}
	return vc
}
