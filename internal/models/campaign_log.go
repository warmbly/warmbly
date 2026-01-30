package models

import (
	"time"

	"github.com/google/uuid"
)

type CampaignLog struct {
	ID         uuid.UUID              `json:"id"`
	CampaignID uuid.UUID              `json:"campaign_id"`
	EventType  string                 `json:"event_type"`
	Message    string                 `json:"message"`
	Metadata   map[string]interface{} `json:"metadata"`
	CreatedAt  time.Time              `json:"created_at"`
}

type CampaignLogsResult struct {
	Data       []CampaignLog `json:"data"`
	Pagination CPagination   `json:"pagination"`
}
