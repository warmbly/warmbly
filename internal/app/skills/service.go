// Package skills manages org AI skills (playbooks): CRUD for the settings UI,
// plus the two hooks the AI features use — an enabled-skills preamble injected
// into every agent/research/reply prompt, and a name lookup backing the
// load_skill tool.
package skills

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type Service interface {
	List(ctx context.Context, orgID uuid.UUID) ([]models.AISkill, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.AISkill, *errx.Error)
	Create(ctx context.Context, orgID uuid.UUID, req *models.CreateAISkill) (*models.AISkill, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, req *models.UpdateAISkill) (*models.AISkill, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error

	// EnabledPreamble renders the enabled skills' name + description for a
	// system prompt (empty when the org has none). The model calls load_skill
	// to read a skill's full content when relevant.
	EnabledPreamble(ctx context.Context, orgID uuid.UUID) string
	// GetByName backs the load_skill tool; returns nil when not found or disabled.
	GetByName(ctx context.Context, orgID uuid.UUID, name string) (*models.AISkill, error)
}

type service struct {
	repo repository.SkillRepository
}

func NewService(repo repository.SkillRepository) Service {
	return &service{repo: repo}
}

func (s *service) List(ctx context.Context, orgID uuid.UUID) ([]models.AISkill, *errx.Error) {
	out, err := s.repo.List(ctx, orgID, false)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to list skills")
	}
	return out, nil
}

func (s *service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.AISkill, *errx.Error) {
	sk, err := s.repo.Get(ctx, orgID, id)
	if err != nil {
		return nil, errx.New(errx.Internal, "failed to read skill")
	}
	if sk == nil {
		return nil, errx.ErrNotFound
	}
	return sk, nil
}

func (s *service) Create(ctx context.Context, orgID uuid.UUID, req *models.CreateAISkill) (*models.AISkill, *errx.Error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errx.New(errx.BadRequest, "name is required")
	}
	if len(req.Content) > models.MaxSkillContentBytes {
		return nil, errx.New(errx.BadRequest, "skill content is too long (max 32KB)")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	sk := &models.AISkill{
		OrgID: orgID, Name: name, Description: strings.TrimSpace(req.Description),
		Content: req.Content, Enabled: enabled,
	}
	out, err := s.repo.Create(ctx, sk)
	if err != nil {
		return nil, errx.New(errx.Conflict, "a skill with that name already exists")
	}
	return out, nil
}

func (s *service) Update(ctx context.Context, orgID, id uuid.UUID, req *models.UpdateAISkill) (*models.AISkill, *errx.Error) {
	sk, xerr := s.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			return nil, errx.New(errx.BadRequest, "name is required")
		}
		sk.Name = n
	}
	if req.Description != nil {
		sk.Description = strings.TrimSpace(*req.Description)
	}
	if req.Content != nil {
		if len(*req.Content) > models.MaxSkillContentBytes {
			return nil, errx.New(errx.BadRequest, "skill content is too long (max 32KB)")
		}
		sk.Content = *req.Content
	}
	if req.Enabled != nil {
		sk.Enabled = *req.Enabled
	}
	if err := s.repo.Update(ctx, sk); err != nil {
		return nil, errx.New(errx.Conflict, "a skill with that name already exists")
	}
	return sk, nil
}

func (s *service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	if err := s.repo.Delete(ctx, orgID, id); err != nil {
		return errx.New(errx.Internal, "failed to delete skill")
	}
	return nil
}

func (s *service) EnabledPreamble(ctx context.Context, orgID uuid.UUID) string {
	skills, err := s.repo.List(ctx, orgID, true)
	if err != nil || len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("YOUR WORKSPACE PLAYBOOKS. These are how this team wants things done. Call load_skill(\"<name>\") to read the full playbook when one is relevant to the task:")
	for _, sk := range skills {
		desc := sk.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "\n- %s: %s", sk.Name, desc)
	}
	return b.String()
}

func (s *service) GetByName(ctx context.Context, orgID uuid.UUID, name string) (*models.AISkill, error) {
	sk, err := s.repo.GetByName(ctx, orgID, name)
	if err != nil {
		return nil, err
	}
	if sk == nil || !sk.Enabled {
		return nil, nil
	}
	return sk, nil
}
