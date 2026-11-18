SET LOCAL lock_timeout = '10s';

-- Review mode: a lead's first email waits until a member approves the lead.
ALTER TABLE public.campaigns ADD COLUMN review_leads boolean NOT NULL DEFAULT false;

-- When a lead started waiting for review; cleared only by an approval or by
-- turning review off. Routing and the send gate read it as a hold, and no other
-- hold's writes touch it.
ALTER TABLE public.campaign_leads ADD COLUMN review_pending_at timestamptz;

-- A waiting lead whose hold slot is free also carries a 'review' hold there,
-- so a release that predates review_pending_at still skips it during a deploy
-- or after a rollback.
ALTER TABLE public.campaign_leads DROP CONSTRAINT IF EXISTS campaign_leads_pause_source_check;
-- NOT VALID: 000402 validates it in its own transaction.
ALTER TABLE public.campaign_leads
    ADD CONSTRAINT campaign_leads_pause_source_check
    CHECK (pause_source IS NULL OR pause_source IN ('manual', 'out_of_office', 'inbox_tagging', 'cc', 'crm', 'review')) NOT VALID;

-- One lead's own copy of one email step. It replaces the step's copy for that
-- lead only and is sent as written, without A/B variants or AI blocks.
CREATE TABLE public.campaign_lead_step_overrides (
    campaign_id uuid NOT NULL,
    contact_id  uuid NOT NULL,
    sequence_id uuid NOT NULL REFERENCES public.sequences (id) ON DELETE CASCADE,
    subject     text NOT NULL DEFAULT '',
    body_html   text NOT NULL DEFAULT '',
    body_plain  text NOT NULL DEFAULT '',
    edited_by   uuid REFERENCES public.users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (campaign_id, contact_id, sequence_id),
    FOREIGN KEY (campaign_id, contact_id) REFERENCES public.campaign_leads (campaign_id, contact_id) ON DELETE CASCADE,
    CONSTRAINT campaign_lead_step_overrides_size CHECK (
        char_length(subject) <= 998
        AND octet_length(body_html) <= 524288
        AND octet_length(body_plain) <= 262144
    )
);

-- Serves the sequences FK's cascade.
CREATE INDEX idx_campaign_lead_step_overrides_sequence ON public.campaign_lead_step_overrides (sequence_id);

-- Whether a lead has been emailed in a campaign, or has a send in flight.
CREATE FUNCTION public.campaign_lead_attempted(p_campaign uuid, p_contact uuid) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1 FROM public.campaign_contact_progress p
        WHERE p.campaign_id = p_campaign AND p.contact_id = p_contact
          AND (p.sent_at IS NOT NULL OR p.dispatched_at IS NOT NULL)
    )
$$;

-- Serializes enrolment with turning review on: enrolments share this lock and
-- the toggle takes it exclusively, so no lead is enrolled against a stale flag.
CREATE FUNCTION public.campaign_review_lock_key(p_campaign uuid) RETURNS bigint
LANGUAGE sql IMMUTABLE AS $$
    SELECT hashtextextended('campaign_review:' || p_campaign::text, 0)
$$;

-- A trigger rather than callers, so every path that enrols a lead (segments,
-- imports, the API, automations) waits for review the same way. An org
-- transfer import sets warmbly.org_import so each lead keeps the state it had.
CREATE FUNCTION public.campaign_lead_enrol_review_hold() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    reviewing boolean;
BEGIN
    IF COALESCE(current_setting('warmbly.org_import', true), '') = 'on' THEN
        RETURN NEW;
    END IF;
    PERFORM pg_advisory_xact_lock_shared(public.campaign_review_lock_key(NEW.campaign_id));
    SELECT c.review_leads INTO reviewing FROM public.campaigns c WHERE c.id = NEW.campaign_id;
    IF reviewing THEN
        NEW.review_pending_at := COALESCE(NEW.review_pending_at, NOW());
        IF NEW.paused_at IS NULL THEN
            NEW.paused_at := NOW();
            NEW.paused_until := NULL;
            NEW.pause_reason := NULL;
            NEW.pause_source := 'review';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- Fires after campaign_lead_enrol_cc_hold (triggers run in name order), so a
-- copied contact keeps its cc hold.
CREATE TRIGGER campaign_lead_enrol_review_hold
    BEFORE INSERT ON public.campaign_leads
    FOR EACH ROW EXECUTE FUNCTION public.campaign_lead_enrol_review_hold();

-- 000230's cc hold, except that a released copy still waiting for review gets
-- its 'review' hold back for releases that read only the hold slot.
CREATE OR REPLACE FUNCTION public.campaign_lead_cc_hold() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        -- A member's own live pause outranks this; anything else is replaced.
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
    -- Released once no lead in the campaign copies them any more.
    UPDATE campaign_leads cl
    SET paused_at = CASE WHEN cl.review_pending_at IS NOT NULL THEN NOW() END,
        paused_until = NULL,
        pause_reason = NULL,
        pause_source = CASE WHEN cl.review_pending_at IS NOT NULL THEN 'review' END
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
