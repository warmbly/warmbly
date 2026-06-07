DROP INDEX IF EXISTS idx_crm_tasks_assigned_team;

ALTER TABLE crm_tasks
    DROP COLUMN IF EXISTS assigned_team_id;
