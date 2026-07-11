ALTER TABLE dedicated_worker_assignments
    DROP CONSTRAINT IF EXISTS dedicated_worker_assignments_organization_id_fkey;

ALTER TABLE dedicated_worker_assignments
    RENAME COLUMN organization_id TO user_id;

-- Map org-keyed rows back to the org owner so the users FK can be restored.
UPDATE dedicated_worker_assignments dwa
SET user_id = o.owner_user_id
FROM organizations o
WHERE o.id = dwa.user_id;

DELETE FROM dedicated_worker_assignments dwa
WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = dwa.user_id);

ALTER TABLE dedicated_worker_assignments
    ADD CONSTRAINT dedicated_worker_assignments_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE dedicated_worker_assignments
    RENAME CONSTRAINT unique_active_org_assignment TO unique_active_user_assignment;

ALTER INDEX idx_dedicated_org RENAME TO idx_dedicated_user;
