DROP TABLE IF EXISTS decision_log;
DROP TABLE IF EXISTS provisioning_jobs;
DROP TABLE IF EXISTS provisioning_policy;
DROP TABLE IF EXISTS provisioning_templates;
-- worker_profiles is owned by migration 000029, not this one — do NOT drop it
-- here or rolling back provisioning would destroy the worker-credentials table.
DROP TABLE IF EXISTS cloud_credentials;
