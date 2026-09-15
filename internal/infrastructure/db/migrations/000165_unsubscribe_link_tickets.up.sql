-- Short opaque unsubscribe links. An opt-out address is the one URL a cold
-- email cannot hide: the text/plain alternative has no anchor to put a word
-- in, so the recipient reads the whole thing (issue #498). The stateless
-- signed token it replaces carried three ids, an expiry and a MAC, which is
-- 96 base64 characters and wraps onto a second line. A ticket is 12, so the
-- address fits on one and stops looking like a tracking parameter.
--
-- One row per recipient per campaign, reused by every step of it, so this is
-- bounded by campaign_leads rather than by messages sent. No foreign keys,
-- like tracked_links: the send path writes it per recipient, and an opt-out
-- link a recipient still holds should outlive the campaign or contact row it
-- was minted for.
CREATE TABLE unsubscribe_links (
    token text PRIMARY KEY,
    organization_id uuid NOT NULL,
    campaign_id uuid NOT NULL,
    contact_id uuid NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone NOT NULL DEFAULT now()
);

-- The mint path upserts on the recipient, so a follow-up months into a
-- sequence reuses the address the first email already carried and only pushes
-- the expiry out. A test send names no contact and keys on the nil uuid.
CREATE UNIQUE INDEX idx_unsubscribe_links_recipient
    ON unsubscribe_links (organization_id, campaign_id, contact_id);
