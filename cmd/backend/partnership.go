package main

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/oauthapp"
	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/models"
)

// connectDescriber names a sign-in link's workspace and app on the page its mailbox owner opens.
type connectDescriber struct {
	orgs organization.OrganizationService
	apps oauthapp.Service
}

func (d connectDescriber) WorkspaceName(ctx context.Context, orgID uuid.UUID) string {
	if d.orgs == nil {
		return ""
	}
	org, xerr := d.orgs.Get(ctx, orgID)
	if xerr != nil || org == nil {
		return ""
	}
	return org.Name
}

func (d connectDescriber) AppName(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) string {
	if d.apps == nil {
		return ""
	}
	apps, xerr := d.apps.List(ctx, orgID)
	if xerr != nil {
		return ""
	}
	for _, a := range apps {
		if a.Provider == provider {
			return a.AppName
		}
	}
	return ""
}
