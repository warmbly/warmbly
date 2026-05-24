package models

import (
	"time"

	"github.com/google/uuid"
)

type Campaign struct {
	ID             uuid.UUID  `json:"id"`
	UserID         string     `json:"user_id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`

	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`

	StopOnReply       bool `json:"stop_on_reply"`
	OpenTracking      bool `json:"open_tracking"`
	LinkTracking      bool `json:"link_tracking"`
	TextOnly          bool `json:"text_only"`
	DailyLimit        int  `json:"daily_limit"`
	UnsubscribeHeader bool `json:"unscrubscribe_header"`
	RiskyEmails       bool `json:"risky_emails"`

	CC  []string `json:"cc"`
	BCC []string `json:"bcc"`

	StartDate *time.Time `json:"start_date"`
	EndDate   *time.Time `json:"end_date"`
	Timezone  string     `json:"timezone"`
	Days      uint8      `json:"days"`
	StartTime string     `json:"start_time"`
	EndTime   string     `json:"end_time"`

	EmailTags []string `json:"email_tags"`
	Folders   []string `json:"folders"`

	ContactOrderBy    string  `json:"contact_order_by"`
	ContactOrderDir   string  `json:"contact_order_dir"`
	ContactOrderField *string `json:"contact_order_field,omitempty"`

	LastStatusChangeAt *time.Time `json:"last_status_change_at,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type MiniCampaign struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CampaignsResult struct {
	Data       []Campaign `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type UpdateCampaign struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status,omitempty"`

	StopOnReply       *bool `json:"stop_on_reply"`
	OpenTracking      *bool `json:"open_tracking"`
	LinkTracking      *bool `json:"link_tracking"`
	TextOnly          *bool `json:"text_only"`
	DailyLimit        *int  `json:"daily_limit"`
	UnsubscribeHeader *bool `json:"unsubscribe_header"`
	RiskyEmails       *bool `json:"risky_emails"`

	CC  []string `json:"cc"`
	BCC []string `json:"bcc"`

	StartDate *time.Time `json:"start_date"`
	EndDate   *time.Time `json:"end_date"`
	Timezone  *string    `json:"timezone"`
	Days      *uint8     `json:"days"`
	StartTime *string    `json:"start_time"`
	EndTime   *string    `json:"end_time"`

	EmailTags []string `json:"email_tags"`
	Folders   []string `json:"folders"`

	ContactOrderBy    *string `json:"contact_order_by"`
	ContactOrderDir   *string `json:"contact_order_dir"`
	ContactOrderField *string `json:"contact_order_field"`
}

type CreateCampaign struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
