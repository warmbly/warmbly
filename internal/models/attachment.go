package models

import (
	"time"

	"github.com/google/uuid"
)

// CampaignAttachment is the metadata + ownership record for an email
// attachment. The binary itself lives in object storage at S3Key.
type CampaignAttachment struct {
	ID         uuid.UUID  `json:"id"`
	CampaignID uuid.UUID  `json:"campaign_id"`
	SequenceID *uuid.UUID `json:"sequence_id,omitempty"`
	UserID     uuid.UUID  `json:"user_id"`
	Filename   string     `json:"filename"`
	Size       int64      `json:"size"`
	MimeType   string     `json:"mime_type"`
	S3Key      string     `json:"s3_key"`
	CreatedAt  time.Time  `json:"created_at"`
}

// AttachmentRef is the lightweight reference carried through the send pipeline
// (backend → Kafka → worker). The worker fetches the bytes from S3 by key.
type AttachmentRef struct {
	S3Key    string `json:"s3_key"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
}
