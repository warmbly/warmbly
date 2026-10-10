package unibox

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type visibilityLabelsRepo struct {
	repository.UniboxRepository
	org     uuid.UUID
	thread  string
	allowed []uuid.UUID
	labels  []models.MiniCategory
	err     error
}

func (r *visibilityLabelsRepo) ListThreadLabelsWithin(_ context.Context, org uuid.UUID, thread string, allowed []uuid.UUID) ([]models.MiniCategory, error) {
	r.org, r.thread, r.allowed = org, thread, allowed
	return r.labels, r.err
}

func TestListThreadLabelsWithinPreservesVisibilityAndUniformNotFound(t *testing.T) {
	org, mailbox := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name    string
		labels  []models.MiniCategory
		err     error
		allowed []uuid.UUID
		code    errx.Code
	}{
		{"mixed grants", []models.MiniCategory{{ID: uuid.New(), Title: "Visible"}}, nil, []uuid.UUID{mailbox, uuid.New()}, 0},
		{"visible unlabelled", []models.MiniCategory{}, nil, []uuid.UUID{mailbox}, 0},
		{"unknown", nil, pgx.ErrNoRows, []uuid.UUID{mailbox}, errx.NotFound},
		{"inaccessible", nil, pgx.ErrNoRows, []uuid.UUID{mailbox}, errx.NotFound},
		{"cross-org", nil, pgx.ErrNoRows, []uuid.UUID{mailbox}, errx.NotFound},
		{"empty grants", nil, pgx.ErrNoRows, []uuid.UUID{}, errx.NotFound},
		{"database error", nil, errors.New("connection failure"), []uuid.UUID{mailbox}, errx.Internal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &visibilityLabelsRepo{labels: tc.labels, err: tc.err}
			s := &uniboxService{uniboxRepository: repo}
			labels, xerr := s.ListThreadLabelsWithin(context.Background(), org, "thread", tc.allowed)
			if tc.code == 0 {
				if xerr != nil || !reflect.DeepEqual(labels, tc.labels) {
					t.Fatalf("labels = %+v, %v", labels, xerr)
				}
			} else if xerr == nil || xerr.Code != tc.code || (tc.code == errx.NotFound && xerr.Message != "thread not found") {
				t.Fatalf("error = %v; want uniform code %v", xerr, tc.code)
			}
			if repo.org != org || repo.thread != "thread" || !reflect.DeepEqual(repo.allowed, tc.allowed) {
				t.Fatalf("query widened: org %s, thread %s, allowed %v", repo.org, repo.thread, repo.allowed)
			}
		})
	}
}
