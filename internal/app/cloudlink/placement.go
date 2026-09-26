package cloudlink

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *service) PlacementPanel(ctx context.Context) (*models.PlacementCloudPanel, *errx.Error) {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	var out models.PlacementCloudPanel
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/placement/panel", nil, &out); xerr != nil {
		return nil, xerr
	}
	return &out, nil
}

func (s *service) StartPlacement(ctx context.Context, req models.PlacementCloudStartRequest) (*models.PlacementCloudStart, *errx.Error) {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	var out models.PlacementCloudStart
	if xerr := s.clientFor(l).do(ctx, http.MethodPost, "/instance/placement/tests", req, &out); xerr != nil {
		return nil, xerr
	}
	return &out, nil
}

func (s *service) ReportPlacementSends(ctx context.Context, testID uuid.UUID, sends []models.PlacementCloudSend) *errx.Error {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return xerr
	}
	return s.clientFor(l).do(ctx, http.MethodPost, "/instance/placement/tests/"+testID.String()+"/sends",
		models.PlacementCloudSends{Sends: sends}, nil)
}

func (s *service) PlacementVerdicts(ctx context.Context, testID uuid.UUID) (*models.PlacementCloudTest, *errx.Error) {
	l, xerr := s.link(ctx)
	if xerr != nil {
		return nil, xerr
	}
	var out models.PlacementCloudTest
	if xerr := s.clientFor(l).do(ctx, http.MethodGet, "/instance/placement/tests/"+testID.String(), nil, &out); xerr != nil {
		return nil, xerr
	}
	return &out, nil
}
