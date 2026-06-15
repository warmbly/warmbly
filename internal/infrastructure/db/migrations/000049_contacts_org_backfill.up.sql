-- Product-created contacts predate organization_id being set on insert, so they
-- are NULL and invisible in the org-scoped contacts list. Backfill from the
-- creating user's organization ONLY when it is unambiguous (the user belongs to
-- exactly one organization). Multi-org users' contacts are left NULL on purpose
-- (no cross-workspace reassignment) and can be migrated manually later.
UPDATE contacts c
SET organization_id = m.organization_id
FROM organization_members m
WHERE c.organization_id IS NULL
  AND m.user_id = c.user_id
  AND (SELECT COUNT(*) FROM organization_members m2 WHERE m2.user_id = c.user_id) = 1;
