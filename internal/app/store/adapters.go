package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v86"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// CustomerReader reads a Stripe customer.
type CustomerReader interface {
	GetCustomer(ctx context.Context, customerID string) (*stripe.Customer, *errx.Error)
}

// SubscriptionReader finds a workspace's Stripe customer.
type SubscriptionReader interface {
	GetByOrganizationID(ctx context.Context, orgID uuid.UUID) (*models.Subscription, error)
}

// StripeBilling reads the billing contact Stripe holds for a workspace.
type StripeBilling struct {
	Subs      SubscriptionReader
	Customers CustomerReader
}

// BillingDetails is the workspace's Stripe customer as a registrant draft; nil without one.
func (b StripeBilling) BillingDetails(ctx context.Context, orgID uuid.UUID) (*BillingDetails, error) {
	if b.Subs == nil || b.Customers == nil {
		return nil, nil
	}
	sub, err := b.Subs.GetByOrganizationID(ctx, orgID)
	if err != nil || sub == nil || sub.StripeCustomerID == "" {
		return nil, err
	}
	c, xerr := b.Customers.GetCustomer(ctx, sub.StripeCustomerID)
	if xerr != nil || c == nil {
		return nil, nil
	}
	out := &BillingDetails{Name: c.Name, Email: c.Email, Phone: c.Phone}
	if a := c.Address; a != nil {
		out.Line1, out.Line2, out.City, out.State, out.PostalCode, out.Country = a.Line1, a.Line2, a.City, a.State, a.PostalCode, a.Country
	}
	return out, nil
}

// UserReader reads a user.
type UserReader interface {
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
}

// OrgReader reads a workspace.
type OrgReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*models.Organization, error)
}

// Directory adapts the user and organization repositories to People.
type Directory struct {
	Users UserReader
	Orgs  OrgReader
}

func (d Directory) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return d.Users.GetUser(ctx, id)
}

func (d Directory) GetOrganization(ctx context.Context, id uuid.UUID) (*models.Organization, error) {
	return d.Orgs.GetByID(ctx, id)
}
