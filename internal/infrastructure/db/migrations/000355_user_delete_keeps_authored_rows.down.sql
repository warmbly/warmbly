-- Restores the plain references, NOT VALID so a rollback takes no scan under write locks. Columns stay nullable:
-- a row a deletion already detached has nobody to name.
ALTER TABLE admin_audit_logs DROP CONSTRAINT IF EXISTS admin_audit_logs_admin_user_id_fkey,
    ADD CONSTRAINT admin_audit_logs_admin_user_id_fkey FOREIGN KEY (admin_user_id) REFERENCES users (id) NOT VALID;
ALTER TABLE admin_outreach_messages DROP CONSTRAINT IF EXISTS admin_outreach_messages_sent_by_fkey,
    ADD CONSTRAINT admin_outreach_messages_sent_by_fkey FOREIGN KEY (sent_by) REFERENCES users (id) NOT VALID;
ALTER TABLE contact_activities DROP CONSTRAINT IF EXISTS contact_activities_user_id_fkey,
    ADD CONSTRAINT contact_activities_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) NOT VALID;
ALTER TABLE contact_notes DROP CONSTRAINT IF EXISTS contact_notes_user_id_fkey,
    ADD CONSTRAINT contact_notes_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) NOT VALID;
ALTER TABLE crm_tasks DROP CONSTRAINT IF EXISTS crm_tasks_assigned_to_fkey,
    ADD CONSTRAINT crm_tasks_assigned_to_fkey FOREIGN KEY (assigned_to) REFERENCES users (id) NOT VALID;
ALTER TABLE crm_tasks DROP CONSTRAINT IF EXISTS crm_tasks_created_by_fkey,
    ADD CONSTRAINT crm_tasks_created_by_fkey FOREIGN KEY (created_by) REFERENCES users (id) NOT VALID;
ALTER TABLE deals DROP CONSTRAINT IF EXISTS deals_assigned_to_fkey,
    ADD CONSTRAINT deals_assigned_to_fkey FOREIGN KEY (assigned_to) REFERENCES users (id) NOT VALID;
ALTER TABLE enterprise_inquiries DROP CONSTRAINT IF EXISTS enterprise_inquiries_assigned_to_fkey,
    ADD CONSTRAINT enterprise_inquiries_assigned_to_fkey FOREIGN KEY (assigned_to) REFERENCES users (id) NOT VALID;
ALTER TABLE enterprise_inquiries DROP CONSTRAINT IF EXISTS enterprise_inquiries_processed_by_fkey,
    ADD CONSTRAINT enterprise_inquiries_processed_by_fkey FOREIGN KEY (processed_by) REFERENCES users (id) NOT VALID;
ALTER TABLE enterprise_inquiries DROP CONSTRAINT IF EXISTS enterprise_inquiries_user_id_fkey,
    ADD CONSTRAINT enterprise_inquiries_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) NOT VALID;
ALTER TABLE limit_increase_requests DROP CONSTRAINT IF EXISTS limit_increase_requests_reviewed_by_fkey,
    ADD CONSTRAINT limit_increase_requests_reviewed_by_fkey FOREIGN KEY (reviewed_by) REFERENCES users (id) NOT VALID;
ALTER TABLE limit_increase_requests DROP CONSTRAINT IF EXISTS limit_increase_requests_submitted_by_fkey,
    ADD CONSTRAINT limit_increase_requests_submitted_by_fkey FOREIGN KEY (submitted_by) REFERENCES users (id) NOT VALID;
ALTER TABLE organization_invitations DROP CONSTRAINT IF EXISTS organization_invitations_invited_by_fkey,
    ADD CONSTRAINT organization_invitations_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES users (id) NOT VALID;
ALTER TABLE organization_members DROP CONSTRAINT IF EXISTS organization_members_invited_by_fkey,
    ADD CONSTRAINT organization_members_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES users (id) NOT VALID;
ALTER TABLE outreach_settings DROP CONSTRAINT IF EXISTS outreach_settings_updated_by_fkey,
    ADD CONSTRAINT outreach_settings_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES users (id) NOT VALID;
ALTER TABLE scheduled_deletions DROP CONSTRAINT IF EXISTS scheduled_deletions_cancelled_by_user_id_fkey,
    ADD CONSTRAINT scheduled_deletions_cancelled_by_user_id_fkey FOREIGN KEY (cancelled_by_user_id) REFERENCES users (id) NOT VALID;
ALTER TABLE scheduled_deletions DROP CONSTRAINT IF EXISTS scheduled_deletions_requested_by_user_id_fkey,
    ADD CONSTRAINT scheduled_deletions_requested_by_user_id_fkey FOREIGN KEY (requested_by_user_id) REFERENCES users (id) NOT VALID;
ALTER TABLE user_bans DROP CONSTRAINT IF EXISTS user_bans_banned_by_fkey,
    ADD CONSTRAINT user_bans_banned_by_fkey FOREIGN KEY (banned_by) REFERENCES users (id) NOT VALID;
ALTER TABLE user_bans DROP CONSTRAINT IF EXISTS user_bans_unbanned_by_fkey,
    ADD CONSTRAINT user_bans_unbanned_by_fkey FOREIGN KEY (unbanned_by) REFERENCES users (id) NOT VALID;
ALTER TABLE user_rate_limits DROP CONSTRAINT IF EXISTS user_rate_limits_updated_by_fkey,
    ADD CONSTRAINT user_rate_limits_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES users (id) NOT VALID;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_admin_granted_by_fkey,
    ADD CONSTRAINT users_admin_granted_by_fkey FOREIGN KEY (admin_granted_by) REFERENCES users (id) NOT VALID;
ALTER TABLE warmup_admin_actions DROP CONSTRAINT IF EXISTS warmup_admin_actions_admin_user_id_fkey,
    ADD CONSTRAINT warmup_admin_actions_admin_user_id_fkey FOREIGN KEY (admin_user_id) REFERENCES users (id) NOT VALID;
ALTER TABLE warmup_appeals DROP CONSTRAINT IF EXISTS warmup_appeals_reviewed_by_fkey,
    ADD CONSTRAINT warmup_appeals_reviewed_by_fkey FOREIGN KEY (reviewed_by) REFERENCES users (id) NOT VALID;
ALTER TABLE warmup_appeals DROP CONSTRAINT IF EXISTS warmup_appeals_user_id_fkey,
    ADD CONSTRAINT warmup_appeals_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) NOT VALID;
