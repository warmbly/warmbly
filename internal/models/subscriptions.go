package models

import (
	"time"

	"github.com/google/uuid"
)

type Duration string

const (
	DurationMonth Duration = "month"
	DurationYear  Duration = "year"
)

type Plan struct {
	ID              uuid.UUID `json:"id"`
	MaxContacts     uint      `json:"max_contacts"`
	DailyEmails     uint      `json:"daily_emails"`
	AIGeneration    bool      `json:"ai_generation"`
	AccountLimit    uint      `json:"account_limit"`
	Price           float32   `json:"price"`
	DiscountedPrice float32   `json:"discounted_price"`
	Duration        Duration  `json:"duration"`
	Savings         uint8     `json:"savings"`
	Public          bool      `json:"public"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type OfferOption struct {
	Title string `json:"title"`
	Plan  string `json:"plan"`
}

type Offer struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Options     []OfferOption `json:"options"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

type Subscription struct {
	ID   string `json:"id"`
	Plan string `json:"plan"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}
