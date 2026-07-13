-- Org voice profile: the free-form grounding folded into every AI writing
-- surface (writing assistant, reply drafts, contact-research openers,
-- automation ai_generate). product_description is what the org sells, icp_notes
-- is who they sell to, voice_profile is their house style. All optional; empty
-- means the AI uses only the built-in humanizer rules.
ALTER TABLE organizations
    ADD COLUMN product_description text NOT NULL DEFAULT '',
    ADD COLUMN icp_notes           text NOT NULL DEFAULT '',
    ADD COLUMN voice_profile       text NOT NULL DEFAULT '';
