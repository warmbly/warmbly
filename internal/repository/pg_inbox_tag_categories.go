package repository

import (
	"context"
	"net/mail"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TagCategoryStore resolves a tag slug to the workspace category row it files
// under. set_thread_labels takes category UUIDs, not strings, so every label
// needs a row before it can ever be applied.
//
// Cached per process: the taxonomy is a fixed list, so this resolves once per
// workspace per boot rather than once per message.
type TagCategoryStore struct {
	db    *pgxpool.Pool
	mu    sync.RWMutex
	cache map[string]uuid.UUID // orgID + "/" + slug -> category id
}

func NewTagCategoryStore(db *pgxpool.Pool) *TagCategoryStore {
	return &TagCategoryStore{db: db, cache: map[string]uuid.UUID{}}
}

// tagColors gives each family a colour so the labels read as a set in the
// inbox rather than a pile of identical chips. Anything unlisted gets slate.
var tagColors = map[string]string{
	"Bounced":        "#b91c1c",
	"Out of office":  "#a16207",
	"Auto-reply":     "#a16207",
	"Notification":   "#64748b",
	"Sales pitch":    "#7c3aed",
	"Interested":     "#15803d",
	"Meeting":        "#15803d",
	"Pricing":        "#0d9488",
	"Question":       "#0284c7",
	"Update":         "#0284c7",
	"Not now":        "#a16207",
	"Not interested": "#9f1239",
	"Wrong person":   "#7c3aed",
	"Unsubscribe":    "#b91c1c",
	"Legal threat":   "#b91c1c",
	"Needs review":   "#c2410c",
	// Follow-up states, warm to cold as the silence lengthens.
	"Needs reply": "#be123c",
	"Follow up":   "#c2410c",
	"Gone quiet":  "#a21caf",
}

const defaultTagColor = "#64748b"

// EnsureCategory returns the category id for a slug, creating the row the first
// time the label is used.
//
// Matched on title, case-insensitively, so a category a person already created
// by hand is adopted rather than duplicated: a workspace that already has an
// "Interested" label keeps using it.
func (s *TagCategoryStore) EnsureCategory(ctx context.Context, orgID uuid.UUID, slug string) (uuid.UUID, error) {
	key := orgID.String() + "/" + slug

	s.mu.RLock()
	if id, ok := s.cache[key]; ok {
		s.mu.RUnlock()
		return id, nil
	}
	s.mu.RUnlock()

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	// Category titles are deliberately not unique, so serialize automatic
	// category creation per workspace to avoid racing into duplicate labels.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, orgID.String()); err != nil {
		return uuid.Nil, err
	}

	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM categories
		WHERE organization_id = $1 AND LOWER(title) = LOWER($2)
		ORDER BY position, id LIMIT 1
	`, orgID, slug).Scan(&id)
	if err != nil && err != pgx.ErrNoRows {
		return uuid.Nil, err
	}
	if err == pgx.ErrNoRows {
		color := tagColors[slug]
		if color == "" {
			color = defaultTagColor
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO categories (organization_id, title, color, position)
			VALUES ($1, $2, $3, COALESCE((SELECT MAX(position) + 1 FROM categories WHERE organization_id = $1), 0))
			RETURNING id
		`, orgID, slug, color).Scan(&id); err != nil {
			return uuid.Nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}

	s.mu.Lock()
	s.cache[key] = id
	s.mu.Unlock()
	return id, nil
}

// EnsureAll creates every label in the taxonomy for a workspace, so the full
// set is visible in the inbox scope rail and selectable in the label filter
// from the moment the feature is switched on.
//
// Without this the labels appear one at a time, as each first fires. A
// workspace could not filter for "opt-out" until something had already opted
// out, which reads as the filter being broken rather than the inbox being
// quiet. Idempotent, so it is safe to call on every boot.
func (s *TagCategoryStore) EnsureAll(ctx context.Context, orgID uuid.UUID, slugs []string) error {
	for _, slug := range slugs {
		if _, err := s.EnsureCategory(ctx, orgID, slug); err != nil {
			return err
		}
	}
	return nil
}

// AddThreadLabels attaches labels without removing any, mirroring the unibox
// repository's own additive path.
//
// Additive is the whole point: a person who labelled a thread "important" must
// not lose it because the classifier ran again. Only the workspace's own
// categories are attached (the SELECT is the guard), and the applier is left
// NULL because there is no human behind an automatic label.
func (s *TagCategoryStore) AddThreadLabels(ctx context.Context, orgID uuid.UUID, threadID string, categoryIDs []uuid.UUID) error {
	if threadID == "" || len(categoryIDs) == 0 {
		return nil
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO unibox_thread_labels (organization_id, thread_id, category_id)
		SELECT $1, $2, c.id
		FROM categories c
		WHERE c.organization_id = $1 AND c.id = ANY($3)
		ON CONFLICT (organization_id, thread_id, category_id) DO NOTHING
	`, orgID, threadID, categoryIDs)
	return err
}

// IsOwnAddress reports whether the sender belongs to this workspace.
func (s *TagCategoryStore) IsOwnAddress(ctx context.Context, orgID uuid.UUID, raw string) (bool, error) {
	address := normalizeMailboxAddress(raw)
	if address == "" {
		return false, nil
	}
	var own bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM email_accounts
			WHERE organization_id = $1 AND LOWER(email) = $2
		)
	`, orgID, address).Scan(&own)
	return own, err
}

func normalizeMailboxAddress(raw string) string {
	address := strings.TrimSpace(raw)
	if parsed, err := mail.ParseAddress(address); err == nil {
		address = parsed.Address
	} else if start, end := strings.LastIndex(address, "("), strings.LastIndex(address, ")"); start >= 0 && end > start {
		address = address[start+1 : end]
	}
	return strings.ToLower(strings.TrimSpace(address))
}

// SyncExclusiveLabels makes `want` the only label from `family` on a thread.
//
// Follow-up labels are not like classification labels: they change as the
// calendar moves and as people reply, so a thread that was awaiting-reply
// yesterday and is follow-up-due today must wear one, not both. Adding without
// removing would leave a thread wearing its whole history.
//
// Only labels in `family` are ever removed. A label a person applied by hand,
// and every classification label, are untouched. An empty `want` removes the
// family entirely, which is the right answer for a thread that has been
// answered or closed.
func (s *TagCategoryStore) SyncExclusiveLabels(ctx context.Context, orgID uuid.UUID, threadID string, family []string, want string) error {
	if threadID == "" || len(family) == 0 {
		return nil
	}

	remove := make([]uuid.UUID, 0, len(family))
	var wantID uuid.UUID
	for _, slug := range family {
		id, err := s.EnsureCategory(ctx, orgID, slug)
		if err != nil {
			return err
		}
		if slug == want {
			wantID = id
		} else {
			remove = append(remove, id)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if len(remove) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM unibox_thread_labels
			WHERE organization_id = $1 AND thread_id = $2
			  AND category_id = ANY($3) AND user_id IS NULL
		`, orgID, threadID, remove); err != nil {
			return err
		}
	}

	if wantID != uuid.Nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO unibox_thread_labels (organization_id, thread_id, category_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (organization_id, thread_id, category_id) DO NOTHING
		`, orgID, threadID, wantID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
