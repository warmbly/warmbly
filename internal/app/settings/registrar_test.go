package settings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

type mockRepo struct {
	byKindProvider func(ctx context.Context, kind, provider string) (*repository.StorageBackend, error)
	create         func(ctx context.Context, b *repository.StorageBackend) error
	update         func(ctx context.Context, id uuid.UUID, name string, cfg json.RawMessage, ro bool) error
	setActive      func(ctx context.Context, id uuid.UUID) error
	calls          []string
}

func (m *mockRepo) List(ctx context.Context) ([]repository.StorageBackend, error) {
	return nil, nil
}
func (m *mockRepo) ListByKind(ctx context.Context, kind string) ([]repository.StorageBackend, error) {
	return nil, nil
}
func (m *mockRepo) GetActive(ctx context.Context, kind string) (*repository.StorageBackend, error) {
	return nil, nil
}
func (m *mockRepo) GetByKindProvider(ctx context.Context, kind, provider string) (*repository.StorageBackend, error) {
	m.calls = append(m.calls, "get:"+kind+":"+provider)
	return m.byKindProvider(ctx, kind, provider)
}
func (m *mockRepo) Create(ctx context.Context, b *repository.StorageBackend) error {
	m.calls = append(m.calls, "create:"+b.Kind+":"+b.Provider)
	return m.create(ctx, b)
}
func (m *mockRepo) UpdateConfig(ctx context.Context, id uuid.UUID, name string, cfg json.RawMessage, ro bool) error {
	m.calls = append(m.calls, "update:"+name)
	return m.update(ctx, id, name, cfg, ro)
}
func (m *mockRepo) SetActive(ctx context.Context, id uuid.UUID) error {
	m.calls = append(m.calls, "activate:"+id.String())
	return m.setActive(ctx, id)
}
func (m *mockRepo) Delete(ctx context.Context, id uuid.UUID) error { return nil }

func TestRegister_CreatesWhenAbsent(t *testing.T) {
	created := false
	activated := false
	id := uuid.New()
	r := NewRegistrar(&mockRepo{
		byKindProvider: func(ctx context.Context, kind, provider string) (*repository.StorageBackend, error) {
			return nil, nil
		},
		create: func(ctx context.Context, b *repository.StorageBackend) error {
			created = true
			b.ID = id
			return nil
		},
		setActive: func(ctx context.Context, gotID uuid.UUID) error {
			if gotID != id {
				t.Fatalf("activate id mismatch: %s vs %s", gotID, id)
			}
			activated = true
			return nil
		},
	})

	err := r.Register(context.Background(), Backend{
		Kind: "kms", Provider: "local", Display: "Local",
		Config: map[string]any{"region": "n/a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || !activated {
		t.Fatalf("expected create + activate; created=%v activated=%v", created, activated)
	}
}

func TestRegister_UpdatesAndActivatesWhenExistsButInactive(t *testing.T) {
	existing := &repository.StorageBackend{ID: uuid.New(), Kind: "blob", Provider: "s3", IsActive: false}
	activated := false
	updated := false
	r := NewRegistrar(&mockRepo{
		byKindProvider: func(ctx context.Context, kind, provider string) (*repository.StorageBackend, error) {
			return existing, nil
		},
		update: func(ctx context.Context, id uuid.UUID, _ string, _ json.RawMessage, _ bool) error {
			if id != existing.ID {
				t.Fatalf("update id mismatch")
			}
			updated = true
			return nil
		},
		setActive: func(ctx context.Context, id uuid.UUID) error {
			if id != existing.ID {
				t.Fatalf("activate id mismatch")
			}
			activated = true
			return nil
		},
	})

	err := r.Register(context.Background(), Backend{Kind: "blob", Provider: "s3", Display: "S3"})
	if err != nil {
		t.Fatal(err)
	}
	if !updated || !activated {
		t.Fatalf("expected update + activate; updated=%v activated=%v", updated, activated)
	}
}

func TestRegister_SkipsActivateWhenAlreadyActive(t *testing.T) {
	existing := &repository.StorageBackend{ID: uuid.New(), Kind: "kms", Provider: "local", IsActive: true}
	activated := false
	r := NewRegistrar(&mockRepo{
		byKindProvider: func(ctx context.Context, kind, provider string) (*repository.StorageBackend, error) {
			return existing, nil
		},
		update: func(ctx context.Context, id uuid.UUID, _ string, _ json.RawMessage, _ bool) error {
			return nil
		},
		setActive: func(ctx context.Context, id uuid.UUID) error {
			activated = true
			return nil
		},
	})

	if err := r.Register(context.Background(), Backend{Kind: "kms", Provider: "local"}); err != nil {
		t.Fatal(err)
	}
	if activated {
		t.Fatal("should not call activate when row is already active")
	}
}

func TestRegister_PropagatesLookupError(t *testing.T) {
	r := NewRegistrar(&mockRepo{
		byKindProvider: func(ctx context.Context, kind, provider string) (*repository.StorageBackend, error) {
			return nil, errors.New("db down")
		},
	})
	err := r.Register(context.Background(), Backend{Kind: "kms", Provider: "local"})
	if err == nil {
		t.Fatal("expected lookup error to propagate")
	}
}

func TestRegisterAll_StopsOnFirstError(t *testing.T) {
	calls := 0
	r := NewRegistrar(&mockRepo{
		byKindProvider: func(ctx context.Context, kind, provider string) (*repository.StorageBackend, error) {
			calls++
			if calls == 2 {
				return nil, errors.New("boom")
			}
			return nil, nil
		},
		create: func(ctx context.Context, b *repository.StorageBackend) error {
			b.ID = uuid.New()
			return nil
		},
		setActive: func(ctx context.Context, id uuid.UUID) error { return nil },
	})
	err := r.RegisterAll(context.Background(), []Backend{
		{Kind: "kms", Provider: "local"},
		{Kind: "blob", Provider: "s3"},
		{Kind: "eventbus", Provider: "kafka"}, // should not be reached
	})
	if err == nil {
		t.Fatal("expected error from second backend")
	}
	if calls != 2 {
		t.Fatalf("expected to stop at 2 lookups, got %d", calls)
	}
}
