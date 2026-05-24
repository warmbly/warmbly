-- Wire contacts to the existing user-scoped `categories` table so a
-- contact can be tagged/labeled. The `categories` table is already
-- shared by the group machinery (folders / tags / categories) and is
-- created in migration 000002. This migration only adds the join.

CREATE TABLE contact_categories (
    contact_id  UUID NOT NULL REFERENCES contacts(id)   ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    created_at  TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (contact_id, category_id)
);

-- Reverse lookup: "give me every contact in this category" — used by
-- the filters API to apply category_ids.
CREATE INDEX idx_contact_categories_category ON contact_categories(category_id);
