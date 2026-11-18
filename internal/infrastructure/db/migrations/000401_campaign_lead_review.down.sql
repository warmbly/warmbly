-- A lead still waiting for review stays held as an indefinite manual pause.
-- Runs before the DDL, so the scan holds no exclusive lock.
UPDATE public.campaign_leads
SET paused_at = COALESCE(review_pending_at, paused_at, NOW()), paused_until = NULL,
    pause_source = 'manual', pause_reason = 'Waiting for review'
WHERE pause_source = 'review'
   OR (review_pending_at IS NOT NULL
       AND NOT (pause_source = 'manual' AND paused_at IS NOT NULL AND paused_until IS NULL));

DROP TRIGGER IF EXISTS campaign_lead_enrol_review_hold ON public.campaign_leads;
DROP FUNCTION IF EXISTS public.campaign_lead_enrol_review_hold();

-- 000230's cc hold as it was.
CREATE OR REPLACE FUNCTION public.campaign_lead_cc_hold() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE campaign_leads cl
        SET paused_at = CASE
                WHEN cl.paused_at IS NOT NULL AND (cl.paused_until IS NULL OR cl.paused_until > NOW())
                THEN cl.paused_at ELSE NOW() END,
            paused_until = NULL,
            pause_reason = (SELECT c.email FROM contacts c WHERE c.id = NEW.contact_id),
            pause_source = 'cc'
        WHERE cl.campaign_id = NEW.campaign_id
          AND cl.contact_id = NEW.cc_contact_id
          AND NOT (cl.pause_source = 'manual' AND cl.paused_at IS NOT NULL
                   AND (cl.paused_until IS NULL OR cl.paused_until > NOW()));
        RETURN NEW;
    END IF;
    UPDATE campaign_leads cl
    SET paused_at = NULL, paused_until = NULL, pause_reason = NULL, pause_source = NULL
    WHERE cl.campaign_id = OLD.campaign_id
      AND cl.contact_id = OLD.cc_contact_id
      AND cl.pause_source = 'cc'
      AND NOT EXISTS (
          SELECT 1 FROM campaign_lead_cc x
          WHERE x.campaign_id = OLD.campaign_id AND x.cc_contact_id = OLD.cc_contact_id
      );
    RETURN OLD;
END;
$$;

DROP FUNCTION IF EXISTS public.campaign_review_lock_key(uuid);
DROP FUNCTION IF EXISTS public.campaign_lead_attempted(uuid, uuid);
DROP TABLE IF EXISTS public.campaign_lead_step_overrides;

ALTER TABLE public.campaign_leads DROP COLUMN IF EXISTS review_pending_at;

ALTER TABLE public.campaign_leads DROP CONSTRAINT IF EXISTS campaign_leads_pause_source_check;
ALTER TABLE public.campaign_leads
    ADD CONSTRAINT campaign_leads_pause_source_check
    CHECK (pause_source IS NULL OR pause_source IN ('manual', 'out_of_office', 'inbox_tagging', 'cc', 'crm')) NOT VALID;

ALTER TABLE public.campaigns DROP COLUMN IF EXISTS review_leads;
