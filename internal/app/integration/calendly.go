package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// CalendlyInvitee mirrors the shape Calendly POSTs on invitee.created.
// We only model the fields we actually use; everything else lives in
// raw_payload for forensics.
type CalendlyPayload struct {
	Event   string `json:"event"`
	Payload struct {
		Invitee struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"invitee"`
		Event struct {
			Name      string    `json:"name"`
			StartTime time.Time `json:"start_time"`
			URI       string    `json:"uri"`
		} `json:"event"`
		ScheduledEvent struct {
			Name      string    `json:"name"`
			StartTime time.Time `json:"start_time"`
			URI       string    `json:"uri"`
		} `json:"scheduled_event"`
		Tracking struct {
			UTMSource string `json:"utm_source"`
			UTMMedium string `json:"utm_medium"`
		} `json:"tracking"`
	} `json:"payload"`
}

// CalComPayload mirrors the Cal.com webhook shape. Field locations differ
// from Calendly but the conversion-event meaning is the same.
type CalComPayload struct {
	TriggerEvent string `json:"triggerEvent"`
	Payload      struct {
		Type      string    `json:"type"`
		Title     string    `json:"title"`
		StartTime time.Time `json:"startTime"`
		Attendees []struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"attendees"`
		UID string `json:"uid"`
	} `json:"payload"`
}

// ContactByEmailFunc is the minimum lookup signature the booking matcher
// needs. Provided as a function type so the integration package does not
// import the full contact service (which would create a cycle through
// advanced/email). The caller adapts whatever signature the repo exposes.
type ContactByEmailFunc func(ctx context.Context, orgID uuid.UUID, email string) (*uuid.UUID, error)

// BookingMatcher joins an inbound booking payload to a Warmbly contact +
// campaign. Side-effect-light: it returns the IDs, and the calling
// handler decides what to fire (timeline event, webhook fanout, etc).
type BookingMatcher struct {
	lookup ContactByEmailFunc
}

func NewBookingMatcher(lookup ContactByEmailFunc) *BookingMatcher {
	return &BookingMatcher{lookup: lookup}
}

func (m *BookingMatcher) MatchContact(ctx context.Context, orgID uuid.UUID, email string) (*uuid.UUID, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || m.lookup == nil {
		return nil, nil
	}
	return m.lookup(ctx, orgID, email)
}

// HandleCalendlyEvent persists the booking + returns the row so the caller
// can fire follow-on events (CRM task, webhook dispatch). Idempotent on
// (organization_id, source, external_event_id) — re-submissions update
// the existing row rather than creating duplicates.
func HandleCalendlyEvent(
	ctx context.Context,
	repo repository.IntegrationRepository,
	matcher *BookingMatcher,
	orgID uuid.UUID,
	body []byte,
) (*models.MeetingBooking, error) {
	var payload CalendlyPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	// Calendly fires events for both create and cancel; we only persist
	// the create path. Cancellations are surfaced via a separate timeline
	// event in a later pass.
	if !strings.EqualFold(payload.Event, "invitee.created") {
		return nil, nil
	}

	eventName := payload.Payload.Event.Name
	if eventName == "" {
		eventName = payload.Payload.ScheduledEvent.Name
	}
	scheduledFor := payload.Payload.Event.StartTime
	if scheduledFor.IsZero() {
		scheduledFor = payload.Payload.ScheduledEvent.StartTime
	}
	eventURI := payload.Payload.Event.URI
	if eventURI == "" {
		eventURI = payload.Payload.ScheduledEvent.URI
	}
	if eventURI == "" {
		return nil, errors.New("calendly payload missing event URI")
	}

	email := strings.ToLower(strings.TrimSpace(payload.Payload.Invitee.Email))
	contactID, _ := matcher.MatchContact(ctx, orgID, email)

	raw, _ := json.Marshal(payload)
	booking := &models.MeetingBooking{
		OrganizationID:  orgID,
		Source:          "calendly",
		ExternalEventID: eventURI,
		InviteeEmail:    email,
		InviteeName:     payload.Payload.Invitee.Name,
		EventName:       eventName,
		ContactID:       contactID,
		RawPayload:      raw,
	}
	if !scheduledFor.IsZero() {
		booking.ScheduledFor = &scheduledFor
	}
	if err := repo.UpsertMeetingBooking(ctx, booking); err != nil {
		return nil, err
	}
	return booking, nil
}

// HandleCalComEvent is the Cal.com counterpart. Same shape, different
// attribute paths.
func HandleCalComEvent(
	ctx context.Context,
	repo repository.IntegrationRepository,
	matcher *BookingMatcher,
	orgID uuid.UUID,
	body []byte,
) (*models.MeetingBooking, error) {
	var payload CalComPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if !strings.EqualFold(payload.TriggerEvent, "BOOKING_CREATED") {
		return nil, nil
	}
	if payload.Payload.UID == "" {
		return nil, errors.New("cal.com payload missing UID")
	}
	if len(payload.Payload.Attendees) == 0 {
		return nil, errors.New("cal.com payload missing attendees")
	}
	primary := payload.Payload.Attendees[0]
	email := strings.ToLower(strings.TrimSpace(primary.Email))
	contactID, _ := matcher.MatchContact(ctx, orgID, email)

	raw, _ := json.Marshal(payload)
	booking := &models.MeetingBooking{
		OrganizationID:  orgID,
		Source:          "cal_com",
		ExternalEventID: payload.Payload.UID,
		InviteeEmail:    email,
		InviteeName:     primary.Name,
		EventName:       payload.Payload.Title,
		ContactID:       contactID,
		RawPayload:      raw,
	}
	if !payload.Payload.StartTime.IsZero() {
		t := payload.Payload.StartTime
		booking.ScheduledFor = &t
	}
	if err := repo.UpsertMeetingBooking(ctx, booking); err != nil {
		return nil, err
	}
	return booking, nil
}
