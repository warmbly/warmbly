-- Dedicated workers are organization assets, and the assignment service has
-- always keyed dedicated_worker_assignments by organization id - but the
-- column was named user_id with a users FK, so every runtime insert failed
-- with an FK violation and paid dedicated-plan orgs could never bind a
-- worker. Re-key the table to organizations.

ALTER TABLE dedicated_worker_assignments
    DROP CONSTRAINT IF EXISTS dedicated_worker_assignments_user_id_fkey;

ALTER TABLE dedicated_worker_assignments
    RENAME COLUMN user_id TO organization_id;

-- Pre-rename rows (seed fixtures) hold owner user ids: remap each to that
-- owner's organization, then drop anything unmappable - such rows were never
-- reachable by the runtime lookups anyway.
UPDATE dedicated_worker_assignments dwa
SET organization_id = o.id
FROM organizations o
WHERE o.owner_user_id = dwa.organization_id
  AND NOT EXISTS (SELECT 1 FROM organizations WHERE id = dwa.organization_id);

DELETE FROM dedicated_worker_assignments dwa
WHERE NOT EXISTS (SELECT 1 FROM organizations o WHERE o.id = dwa.organization_id);

ALTER TABLE dedicated_worker_assignments
    ADD CONSTRAINT dedicated_worker_assignments_organization_id_fkey
    FOREIGN KEY (organization_id) REFERENCES organizations(id) ON DELETE CASCADE;

ALTER TABLE dedicated_worker_assignments
    RENAME CONSTRAINT unique_active_user_assignment TO unique_active_org_assignment;

ALTER INDEX idx_dedicated_user RENAME TO idx_dedicated_org;
