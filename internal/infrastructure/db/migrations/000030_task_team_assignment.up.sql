-- A CRM task can be assigned to an individual user (assigned_to) AND/OR a team.
-- The two are independent: a task may have a person, a team, both, or neither.
-- ON DELETE SET NULL mirrors the soft-clearing behaviour of the user assignment:
-- deleting the team unassigns the task rather than deleting it.
ALTER TABLE crm_tasks
    ADD COLUMN assigned_team_id uuid REFERENCES teams (id) ON DELETE SET NULL;

-- Filtering and counting by team + status is the hot path for the tasks views
-- (team filter + status facets share the search/summary predicate).
CREATE INDEX idx_crm_tasks_assigned_team ON crm_tasks (assigned_team_id, status);
