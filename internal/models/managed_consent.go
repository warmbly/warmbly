package models

import (
	"time"

	"github.com/google/uuid"
)

const ManagedConsentProtocol = 1

type CloudManagedConsent struct {
	ID               uuid.UUID
	OrganizationID   uuid.UUID
	UserID           *uuid.UUID
	InstanceID       *uuid.UUID
	RemoteID         *uuid.UUID
	SessionHash      *string
	Kind             string
	Provider         InboxProvider
	CloudAccountID   *uuid.UUID
	PlannedAccountID *uuid.UUID
	AccountID        *uuid.UUID
	State            string
	CreatedAt        time.Time
	ExpiresAt        time.Time
}

type PoolLinkManagedOperation struct {
	ActivationPending bool
	ID                uuid.UUID
	OrganizationID    uuid.UUID
	InstanceID        *uuid.UUID
	RemoteID          *uuid.UUID
	SessionHash       *string
	Kind              string
	Provider          InboxProvider
	PlannedAccountID  *uuid.UUID
	AccountID         *uuid.UUID
	State             string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	CompletedAt       *time.Time
}
