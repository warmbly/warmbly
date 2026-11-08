package agency

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// AdminAgencyDetail is one agency as the admin panel shows it.
type AdminAgencyDetail struct {
	Agency  models.AdminAgency    `json:"agency"`
	Members []models.AgencyMember `json:"members"`
	Clients []models.AgencyClient `json:"clients"`
	Usage   *models.AgencyUsage   `json:"usage"`
	// AIBalance is the shared AI balance; Unmetered where AI is not charged.
	AIBalance *models.AgencyAIBalance `json:"ai_balance"`
}

// AdminList lists agencies with their plan, status, client count and pool use.
func (s *Service) AdminList(ctx context.Context, search string, limit, offset int) ([]models.AdminAgency, int, *errx.Error) {
	limit = min(max(limit, 1), 100)
	offset = max(offset, 0)
	list, total, err := s.repo.ListForAdmin(ctx, search, limit, offset)
	if err != nil {
		return nil, 0, internal(err)
	}
	for i := range list {
		a := &list[i].Agency
		s.hydrate(ctx, a)
		list[i].PoolUsed, list[i].PoolTotal, list[i].Unlimited = s.PoolTotals(ctx, a)
	}
	if list == nil {
		list = []models.AdminAgency{}
	}
	return list, total, nil
}

// AdminGet is one agency with its staff, clients and pool.
func (s *Service) AdminGet(ctx context.Context, agencyID uuid.UUID) (*AdminAgencyDetail, *errx.Error) {
	a, err := s.repo.Get(ctx, agencyID)
	if err != nil {
		return nil, internal(err)
	}
	if a == nil {
		return nil, errNotFound
	}
	s.hydrate(ctx, a)
	out := &AdminAgencyDetail{Agency: models.AdminAgency{Agency: *a}}
	if owner, err := s.users.GetUser(ctx, a.OwnerUserID); err == nil && owner != nil {
		out.Agency.OwnerEmail = owner.Email
	}
	if out.Members, err = s.repo.ListMembers(ctx, agencyID); err != nil {
		return nil, internal(err)
	}
	if out.Clients, err = s.repo.ListClientOrganizations(ctx, agencyID); err != nil {
		return nil, internal(err)
	}
	usage, xerr := s.usage(ctx, a)
	if xerr != nil {
		return nil, xerr
	}
	out.Usage = usage
	if out.AIBalance, xerr = s.aiBalance(ctx, a); xerr != nil {
		return nil, xerr
	}
	out.Agency.PoolUsed, out.Agency.PoolTotal, out.Agency.Unlimited = usage.Used, usage.Total, usage.Unlimited
	if out.Members == nil {
		out.Members = []models.AgencyMember{}
	}
	if out.Clients == nil {
		out.Clients = []models.AgencyClient{}
	}
	return out, nil
}
