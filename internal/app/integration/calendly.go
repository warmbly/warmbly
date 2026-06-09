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

// MeetingLifecycle is the kind of change an inbound booking webhook represents.
// The handler maps it to the matching webhook + realtime event and timeline
// entry. An empty lifecycle means "an event we deliberately ignore".
type MeetingLifecycle string

const (
	LifecycleBooked      MeetingLifecycle = "booked"
	LifecycleRescheduled MeetingLifecycle = "rescheduled"
	LifecycleCanceled    MeetingLifecycle = "canceled"
	LifecycleIgnore      MeetingLifecycle = ""
)

// CalendlyPayload mirrors the shape Calendly POSTs. The invitee fields live at
// the payload root (not under an "invitee" key); the call detail lives under
// scheduled_event. We model only what we use; everything else stays in
// raw_payload for forensics.
type CalendlyPayload struct {
	Event   string `json:"event"`
	Payload struct {
		Email         string `json:"email"`
		Name          string `json:"name"`
		Status        string `json:"status"`
		CancelURL     string `json:"cancel_url"`
		RescheduleURL string `json:"reschedule_url"`
		Rescheduled   bool   `json:"rescheduled"`
		URI           string `json:"uri"`
		Tracking      struct {
			UTMSource   string `json:"utm_source"`
			UTMMedium   string `json:"utm_medium"`
			UTMCampaign string `json:"utm_campaign"`
			UTMContent  string `json:"utm_content"`
		} `json:"tracking"`
		Cancellation struct {
			Reason     string `json:"reason"`
			CanceledBy string `json:"canceled_by"`
		} `json:"cancellation"`
		ScheduledEvent struct {
			Name      string    `json:"name"`
			StartTime time.Time `json:"start_time"`
			EndTime   time.Time `json:"end_time"`
			URI       string    `json:"uri"`
			EventType string    `json:"event_type"`
			Location  struct {
				Type     string `json:"type"`
				JoinURL  string `json:"join_url"`
				Location string `json:"location"`
			} `json:"location"`
		} `json:"scheduled_event"`
	} `json:"payload"`
}

// CalComPayload mirrors the Cal.com webhook shape. Field locations differ from
// Calendly but the lifecycle meaning is the same.
type CalComPayload struct {
	TriggerEvent string `json:"triggerEvent"`
	Payload      struct {
		Type      string    `json:"type"`
		Title     string    `json:"title"`
		StartTime time.Time `json:"startTime"`
		EndTime   time.Time `json:"endTime"`
		UID       string    `json:"uid"`
		Location  string    `json:"location"`
		Attendees []struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"attendees"`
		Organizer struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"organizer"`
		Metadata struct {
			VideoCallURL string `json:"videoCallUrl"`
		} `json:"metadata"`
		VideoCallData struct {
			URL string `json:"url"`
		} `json:"videoCallData"`
		// responses carries the prefilled booking-form answers, including any
		// hidden contact-id field we embed in the "Book a call" link.
		Responses          map[string]any `json:"responses"`
		CancellationReason string         `json:"cancellationReason"`
		RescheduleUID      string         `json:"rescheduleUid"`
	} `json:"payload"`
}

// ContactByEmailFunc resolves a Warmbly contact id from an email, scoped to the
// org. Provided as a function type so this package does not import the full
// contact service (which would create an import cycle).
type ContactByEmailFunc func(ctx context.Context, orgID uuid.UUID, email string) (*uuid.UUID, error)

// ContactVerifyFunc confirms a contact id actually belongs to the org. Used to
// safely honour a contact-id hint embedded in the booking link (the webhook is
// authenticated by the org's inbound secret, but we still never trust an
// attacker-supplied id without verifying org ownership).
type ContactVerifyFunc func(ctx context.Context, orgID, contactID uuid.UUID) (bool, error)

// BookingMatcher joins an inbound booking payload to a Warmbly contact. It
// prefers a verified id hint (deterministic, survives a different reply-to
// email) and falls back to an org-scoped email lookup.
type BookingMatcher struct {
	lookup ContactByEmailFunc
	verify ContactVerifyFunc
}

func NewBookingMatcher(lookup ContactByEmailFunc, verify ContactVerifyFunc) *BookingMatcher {
	return &BookingMatcher{lookup: lookup, verify: verify}
}

// Resolve returns the best contact id for a booking: a verified hint if present,
// otherwise the email match. Returns nil when neither resolves.
func (m *BookingMatcher) Resolve(ctx context.Context, orgID uuid.UUID, email, idHint string) *uuid.UUID {
	if id := m.matchHint(ctx, orgID, idHint); id != nil {
		return id
	}
	return m.matchEmail(ctx, orgID, email)
}

func (m *BookingMatcher) matchHint(ctx context.Context, orgID uuid.UUID, idHint string) *uuid.UUID {
	idHint = strings.TrimSpace(idHint)
	if idHint == "" || m.verify == nil {
		return nil
	}
	id, err := uuid.Parse(idHint)
	if err != nil {
		return nil
	}
	ok, verr := m.verify(ctx, orgID, id)
	if verr != nil || !ok {
		return nil
	}
	return &id
}

func (m *BookingMatcher) matchEmail(ctx context.Context, orgID uuid.UUID, email string) *uuid.UUID {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || m.lookup == nil {
		return nil
	}
	id, err := m.lookup(ctx, orgID, email)
	if err != nil {
		return nil
	}
	return id
}

// HandleCalendlyEvent parses, attributes, and upserts a Calendly booking,
// returning the row + its lifecycle so the caller can fire follow-on events.
// Idempotent on (organization_id, source, external_event_id) — a re-delivery
// updates the existing row rather than creating duplicates. Calendly models a
// reschedule as cancel-old + create-new, so we ignore the canceled half of a
// reschedule and let the new invitee.created carry the "rescheduled" signal.
func HandleCalendlyEvent(
	ctx context.Context,
	repo repository.IntegrationRepository,
	matcher *BookingMatcher,
	orgID uuid.UUID,
	body []byte,
) (*models.MeetingBooking, MeetingLifecycle, error) {
	var p CalendlyPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, LifecycleIgnore, err
	}

	var lifecycle MeetingLifecycle
	var status models.MeetingBookingStatus
	switch strings.ToLower(strings.TrimSpace(p.Event)) {
	case "invitee.created":
		if p.Payload.Rescheduled {
			lifecycle, status = LifecycleRescheduled, models.MeetingRescheduled
		} else {
			lifecycle, status = LifecycleBooked, models.MeetingBooked
		}
	case "invitee.canceled":
		// The canceled half of a reschedule is noise; the paired
		// invitee.created already represents the move.
		if p.Payload.Rescheduled {
			return nil, LifecycleIgnore, nil
		}
		lifecycle, status = LifecycleCanceled, models.MeetingCanceled
	default:
		return nil, LifecycleIgnore, nil
	}

	se := p.Payload.ScheduledEvent
	if se.URI == "" {
		return nil, LifecycleIgnore, errors.New("calendly payload missing scheduled_event URI")
	}

	email := strings.ToLower(strings.TrimSpace(p.Payload.Email))
	contactID := matcher.Resolve(ctx, orgID, email, p.Payload.Tracking.UTMContent)

	raw, _ := json.Marshal(p)
	booking := &models.MeetingBooking{
		OrganizationID:  orgID,
		Source:          "calendly",
		ExternalEventID: se.URI,
		Status:          status,
		InviteeEmail:    email,
		InviteeName:     p.Payload.Name,
		EventName:       se.Name,
		EventType:       se.EventType,
		JoinURL:         se.Location.JoinURL,
		Location:        calendlyLocation(se.Location.Type, se.Location.Location),
		CancelURL:       p.Payload.CancelURL,
		RescheduleURL:   p.Payload.RescheduleURL,
		CanceledReason:  p.Payload.Cancellation.Reason,
		ContactID:       contactID,
		RawPayload:      raw,
	}
	if !se.StartTime.IsZero() {
		t := se.StartTime
		booking.ScheduledFor = &t
	}
	if !se.EndTime.IsZero() {
		t := se.EndTime
		booking.EndTime = &t
	}
	if err := repo.UpsertMeetingBooking(ctx, booking); err != nil {
		return nil, LifecycleIgnore, err
	}
	return booking, lifecycle, nil
}

// HandleCalComEvent is the Cal.com counterpart. Cal.com emits distinct triggers
// for each lifecycle change, so the mapping is direct.
func HandleCalComEvent(
	ctx context.Context,
	repo repository.IntegrationRepository,
	matcher *BookingMatcher,
	orgID uuid.UUID,
	body []byte,
) (*models.MeetingBooking, MeetingLifecycle, error) {
	var p CalComPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, LifecycleIgnore, err
	}

	var lifecycle MeetingLifecycle
	var status models.MeetingBookingStatus
	switch strings.ToUpper(strings.TrimSpace(p.TriggerEvent)) {
	case "BOOKING_CREATED":
		lifecycle, status = LifecycleBooked, models.MeetingBooked
	case "BOOKING_RESCHEDULED":
		lifecycle, status = LifecycleRescheduled, models.MeetingRescheduled
	case "BOOKING_CANCELLED":
		lifecycle, status = LifecycleCanceled, models.MeetingCanceled
	default:
		return nil, LifecycleIgnore, nil
	}

	if p.Payload.UID == "" {
		return nil, LifecycleIgnore, errors.New("cal.com payload missing UID")
	}
	if len(p.Payload.Attendees) == 0 {
		return nil, LifecycleIgnore, errors.New("cal.com payload missing attendees")
	}

	primary := p.Payload.Attendees[0]
	email := strings.ToLower(strings.TrimSpace(primary.Email))
	contactID := matcher.Resolve(ctx, orgID, email, calComContactHint(p.Payload.Responses))

	joinURL := p.Payload.Metadata.VideoCallURL
	if joinURL == "" {
		joinURL = p.Payload.VideoCallData.URL
	}

	raw, _ := json.Marshal(p)
	booking := &models.MeetingBooking{
		OrganizationID:  orgID,
		Source:          "cal_com",
		ExternalEventID: p.Payload.UID,
		Status:          status,
		InviteeEmail:    email,
		InviteeName:     primary.Name,
		EventName:       p.Payload.Title,
		EventType:       p.Payload.Type,
		JoinURL:         joinURL,
		Location:        p.Payload.Location,
		CanceledReason:  p.Payload.CancellationReason,
		ContactID:       contactID,
		RawPayload:      raw,
	}
	if !p.Payload.StartTime.IsZero() {
		t := p.Payload.StartTime
		booking.ScheduledFor = &t
	}
	if !p.Payload.EndTime.IsZero() {
		t := p.Payload.EndTime
		booking.EndTime = &t
	}
	if err := repo.UpsertMeetingBooking(ctx, booking); err != nil {
		return nil, LifecycleIgnore, err
	}
	return booking, lifecycle, nil
}

// calendlyLocation renders a human location string from Calendly's location
// object. Video calls carry the link in JoinURL (stored separately); physical /
// phone locations carry the address/number here.
func calendlyLocation(locType, location string) string {
	if location != "" {
		return location
	}
	switch locType {
	case "google_conference":
		return "Google Meet"
	case "zoom_conference":
		return "Zoom"
	case "microsoft_teams_conference":
		return "Microsoft Teams"
	case "gotomeeting":
		return "GoToMeeting"
	case "":
		return ""
	default:
		return strings.ReplaceAll(locType, "_", " ")
	}
}

// calComContactHint pulls a contact-id hint out of the Cal.com booking-form
// responses. We embed it in the "Book a call" link under a few common keys; the
// value is only used after org-ownership verification.
func calComContactHint(responses map[string]any) string {
	if responses == nil {
		return ""
	}
	for _, key := range []string{"warmbly_contact_id", "contact_id", "utm_content"} {
		if v, ok := responses[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
			// Cal.com sometimes wraps answers as { value: "..." }.
			if m, ok := v.(map[string]any); ok {
				if s, ok := m["value"].(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}
