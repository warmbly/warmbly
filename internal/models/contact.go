package models

import (
	"time"

	"github.com/google/uuid"
)

type Contact struct {
	ID uuid.UUID `json:"id"`

	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Company   string `json:"company"`
	Phone     string `json:"phone"`

	CustomFields map[string]string `json:"custom_fields"`

	Subscribed bool           `json:"subscribed"`
	Campaigns  []MiniCampaign `json:"campaigns"`
	Categories []string       `json:"categories,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type ContactsResult struct {
	Data       []Contact  `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type UpdateContact struct {
	FirstName        *string            `json:"first_name"`
	LastName         *string            `json:"last_name"`
	Company          *string            `json:"company"`
	Phone            *string            `json:"phone"`
	CustomFields     *map[string]string `json:"custom_fields"`
	Subscribed       *bool              `json:"subscribed"`
	Campaigns        []string           `json:"campaigns"` // List of campaign IDs to set
	AddCategories    []string           `json:"add_categories,omitempty"`
	RemoveCategories []string           `json:"remove_categories,omitempty"`
}

type AddContact struct {
	FirstName  string   `json:"first_name"`
	LastName   string   `json:"last_name"`
	Email      string   `json:"email"`
	Company    string   `json:"company"`
	Phone      string   `json:"phone"`
	Campaigns  []string `json:"campaigns"`
	Categories []string `json:"categories,omitempty"`

	CustomFields map[string]string `json:"custom_fields"`
}

type SearchContactsFilterType string

const (
	SearchContactsFilterTypeEqual      SearchContactsFilterType = "equal"
	SearchContactsFilterTypeStartsWith SearchContactsFilterType = "starts_with"
	SearchContactsFilterTypeEndsWith   SearchContactsFilterType = "ends_with"
	SearchContactsFilterTypeContains   SearchContactsFilterType = "contains"
)

type SearchContactsFilter struct {
	Name  string                   `json:"name"`
	Value string                   `json:"value"`
	Type  SearchContactsFilterType `json:"type"`
}

type SearchContacts struct {
	Query              string                 `json:"query"`                // Text search across core fields
	CustomFieldFilters []SearchContactsFilter `json:"custom_field_filters"` // Custom Field Filters
	CampaignIDs        []string               `json:"campaign_ids"`         // Contacts must be in ALL these campaigns
	CategoryIDs        []string               `json:"category_ids"`         // Contacts must have ALL these categories
	MinCampaigns       *int                   `json:"min_campaigns"`        // Minimum number of associated campaigns
	MaxCampaigns       *int                   `json:"max_campaigns"`        // Maximum number of associated campaigns
	Subscribed         *bool                  `json:"subscribed"`           // Filter by subscription status
	CreatedAfter       *time.Time             `json:"created_after"`        // Contacts created after this date
	CreatedBefore      *time.Time             `json:"created_before"`       // Contacts created before this date
	UpdatedAfter       *time.Time             `json:"updated_after"`        // Contacts updated after this date
	UpdatedBefore      *time.Time             `json:"updated_before"`       // Contacts updated before this date
	SortBy             string                 `json:"sort_by"`              // e.g., "first_name ASC", "campaign_count DESC"
	Reverse            bool                   `json:"reverse"`              // ASC or DESC
	Offset             int                    `json:"offset"`               // Pagination
}

type BulkEditContactsFieldType string

const (
	BulkAddField    BulkEditContactsFieldType = "ADD"
	BulkEditField   BulkEditContactsFieldType = "EDIT"
	BulkDeleteField BulkEditContactsFieldType = "DELETE"
	BulkRenameField BulkEditContactsFieldType = "RENAME"
)

type BulkEditContactsField struct {
	Type  BulkEditContactsFieldType `json:"type"`
	Key   string                    `json:"key"`
	Value string                    `json:"value"`
}

type BulkEditContactsData struct {
	Contacts []string `json:"contacts"`

	AddCampaigns     []string                `json:"add_campaigns"`
	RemoveCampaigns  []string                `json:"remove_campaigns"`
	AddCategories    []string                `json:"add_categories,omitempty"`
	RemoveCategories []string                `json:"remove_categories,omitempty"`
	Fields           []BulkEditContactsField `json:"fields"`
	Subscribe        *bool                   `json:"subscribe"`
}
