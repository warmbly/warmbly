package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/app/whatsapp"
	"github.com/warmbly/warmbly/internal/app/whatsapp/evolution"
	"github.com/warmbly/warmbly/internal/models"
)

// EvolutionWebhook is the public ingress for Evolution API events.
// POST /api/v1/webhooks/evolution/:instance
//
// Auth: Authorization Bearer <secret> or X-Webhook-Secret.
// Requires a mapped whatsapp_instances row (organization binding). Production
// never silently drops CRM effects into "lab_mode".
func (h *Handler) EvolutionWebhook(c *gin.Context) {
	if h.WhatsAppService == nil || !h.WhatsAppService.Config().Enabled {
		c.JSON(http.StatusNotFound, gin.H{"error": "whatsapp channel disabled", "code": "disabled"})
		return
	}
	cfg := h.WhatsAppService.Config()
	instance := strings.TrimSpace(c.Param("instance"))
	if instance == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instance required", "code": "missing_instance"})
		return
	}

	maxBytes := cfg.MaxWebhookBytes
	if maxBytes <= 0 {
		maxBytes = whatsapp.DefaultMaxWebhookBytes
	}
	if c.Request.ContentLength > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "payload too large", "code": "payload_too_large"})
		return
	}

	if h.WhatsAppRepo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "whatsapp repository unavailable", "code": "no_repo"})
		return
	}

	inst, xerr := h.WhatsAppRepo.GetInstanceByName(c.Request.Context(), whatsapp.ProviderEvolution, instance)
	if xerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "lookup failed", "code": "internal"})
		return
	}
	if inst == nil {
		// Fail closed: unmapped instance never reaches CRM/outcomes.
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not mapped", "code": "instance_mismatch"})
		return
	}
	orgID := inst.OrganizationID
	secret := inst.WebhookSecret
	if secret == "" {
		secret = cfg.WebhookSecret
	}
	if secret == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "webhook secret not configured", "code": "missing_secret"})
		return
	}
	if inst.IntegrationMode == "WHATSAPP-BAILEYS" && cfg.IsProduction() {
		log.Error().Str("instance", instance).Msg("rejecting baileys instance webhook in production")
		c.JSON(http.StatusForbidden, gin.H{"error": "baileys disabled in production", "code": "baileys_forbidden"})
		return
	}

	auth := whatsapp.WebhookAuth{Secret: secret, MaxBytes: maxBytes}
	if err := auth.ValidateHeaders(
		c.GetHeader("Authorization"),
		c.GetHeader("X-Webhook-Secret"),
		c.GetHeader("Content-Type"),
		c.Request.ContentLength,
	); err != nil {
		if ae, ok := err.(*whatsapp.AuthError); ok {
			c.JSON(ae.StatusCode, gin.H{"error": ae.Message, "code": ae.Code})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "unauthorized"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed", "code": "bad_body"})
		return
	}
	if int64(len(body)) > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "payload too large", "code": "payload_too_large"})
		return
	}

	ev, err := evolution.NormalizeWebhook(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "malformed payload", "code": "malformed"})
		return
	}
	ev.Instance = instance
	ev.OrganizationID = orgID
	if ev.EventType == whatsapp.EventUnsupported {
		c.JSON(http.StatusOK, gin.H{"received": true, "ignored": true})
		return
	}

	sum := sha256.Sum256(body)
	inserted, xerr := h.WhatsAppRepo.InsertWebhookEvent(
		c.Request.Context(), orgID, whatsapp.ProviderEvolution,
		ev.IdempotencyKey(), ev.EventType, ev.ExternalMessageID, hex.EncodeToString(sum[:]),
	)
	if xerr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "idempotency failed", "code": "internal"})
		return
	}
	if !inserted {
		c.JSON(http.StatusOK, gin.H{"received": true, "duplicate": true})
		return
	}

	// Prefer confenge orchestration when wired (stops sequences, outcomes, CRM).
	if h.ConfengeService != nil && h.ConfengeService.Enabled() && ev.EventType == whatsapp.EventMessageReceived {
		inRes, herr := h.ConfengeService.HandleWhatsAppInbound(c.Request.Context(), orgID, ev)
		if herr != nil {
			log.Error().Err(herr).Str("instance", instance).Msg("confenge whatsapp inbound failed")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "inbound processing failed", "code": "process_failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"received":       true,
			"event_type":     ev.EventType,
			"stop_sequences": inRes.StopSequences,
			"needs_review":   inRes.NeedsHumanReview,
			"opt_out":        inRes.OptOut.Matched && inRes.OptOut.Confident,
			"duplicate":      inRes.Duplicate,
		})
		return
	}

	// Fallback without confenge: persist message + open service window only.
	phone := ev.FromE164
	if phone == "" {
		phone = ev.ToE164
	}
	domainState := whatsapp.ContactChannelState{OrganizationID: orgID, PhoneE164: phone, ConsentStatus: whatsapp.ConsentUnknown}
	if st, _ := h.WhatsAppRepo.GetContactStateByPhone(c.Request.Context(), orgID, phone); st != nil {
		domainState = modelsToDomainState(st, orgID)
	}
	inRes, _ := h.WhatsAppService.ProcessInbound(c.Request.Context(), &domainState, ev)
	if !inRes.Duplicate && ev.EventType == whatsapp.EventMessageReceived {
		msg := &models.WhatsAppMessage{
			OrganizationID:    orgID,
			ThreadKey:         phone,
			Direction:         "inbound",
			Channel:           whatsapp.ChannelWhatsApp,
			Provider:          whatsapp.ProviderEvolution,
			ProviderMessageID: ev.ExternalMessageID,
			IdempotencyKey:    ev.IdempotencyKey(),
			BodyText:          ev.Content.Text,
			Status:            "received",
			OccurredAt:        ev.OccurredAt,
		}
		_, _ = h.WhatsAppRepo.InsertMessage(c.Request.Context(), msg)
		persist := domainToModelsState(domainState, nil)
		persist.OrganizationID = orgID
		_ = h.WhatsAppRepo.UpsertContactState(c.Request.Context(), &persist)
	}
	c.JSON(http.StatusOK, gin.H{
		"received":       true,
		"event_type":     ev.EventType,
		"stop_sequences": inRes.StopSequences,
		"opt_out":        inRes.OptOut.Matched && inRes.OptOut.Confident,
		"duplicate":      inRes.Duplicate,
	})
}

func modelsToDomainState(st *models.WhatsAppContactState, orgID uuid.UUID) whatsapp.ContactChannelState {
	out := whatsapp.ContactChannelState{
		OrganizationID: orgID,
		ConsentStatus:  whatsapp.ConsentUnknown,
	}
	if st == nil {
		return out
	}
	if st.ContactID != nil {
		out.ContactID = *st.ContactID
	}
	out.PhoneE164 = st.PhoneE164
	out.PhoneSource = st.PhoneSource
	out.PhoneSourceURL = st.PhoneSourceURL
	out.PhoneVerifiedAt = st.PhoneVerifiedAt
	out.ConsentStatus = st.ConsentStatus
	out.ConsentSource = st.ConsentSource
	out.ConsentAt = st.ConsentAt
	out.ConsentScope = st.ConsentScope
	out.ConsentProvenanceOK = st.ConsentProvenanceOK
	out.LastInboundAt = st.LastInboundAt
	out.ServiceWindowUntil = st.ServiceWindowUntil
	out.ChannelStatus = st.ChannelStatus
	out.OptOutAt = st.OptOutAt
	out.DoNotContact = st.DoNotContact
	out.LastEmailOutboundAt = st.LastEmailOutboundAt
	out.LastWhatsAppOutboundAt = st.LastWhatsAppOutboundAt
	return out
}

func domainToModelsState(d whatsapp.ContactChannelState, prev *models.WhatsAppContactState) models.WhatsAppContactState {
	out := models.WhatsAppContactState{
		OrganizationID:         d.OrganizationID,
		PhoneE164:              d.PhoneE164,
		PhoneSource:            d.PhoneSource,
		PhoneSourceURL:         d.PhoneSourceURL,
		PhoneVerifiedAt:        d.PhoneVerifiedAt,
		ConsentStatus:          d.ConsentStatus,
		ConsentSource:          d.ConsentSource,
		ConsentAt:              d.ConsentAt,
		ConsentScope:           d.ConsentScope,
		ConsentProvenanceOK:    d.ConsentProvenanceOK,
		LastInboundAt:          d.LastInboundAt,
		ServiceWindowUntil:     d.ServiceWindowUntil,
		ChannelStatus:          d.ChannelStatus,
		OptOutAt:               d.OptOutAt,
		DoNotContact:           d.DoNotContact,
		LastEmailOutboundAt:    d.LastEmailOutboundAt,
		LastWhatsAppOutboundAt: d.LastWhatsAppOutboundAt,
		UpdatedAt:              time.Now().UTC(),
	}
	if d.ContactID != uuid.Nil {
		id := d.ContactID
		out.ContactID = &id
	}
	if prev != nil {
		out.PhoneRaw = prev.PhoneRaw
		out.PhoneCountry = prev.PhoneCountry
		out.PhoneValid = prev.PhoneValid
	}
	return out
}
