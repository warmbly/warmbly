-- Campaign follow-ups are replies, not new cold emails. Every step after the
-- contact's first went out with no In-Reply-To, no References and, on Gmail, no
-- threadId, so each one opened its own conversation and a recipient read the
-- nudge as a second stranger (issue #472).
--
-- Two columns make threading possible:

-- The per-step switch. Default true because replying in the thread is what the
-- dashboard, the docs and every other sequencer already promise; a step that
-- should deliberately open a fresh conversation turns it off.
ALTER TABLE sequences
    ADD COLUMN IF NOT EXISTS thread_reply boolean NOT NULL DEFAULT true;

-- The provider-side conversation handle the worker reports after a send. Gmail
-- only appends to an existing thread when the outbound message carries its
-- threadId; matching Subject and In-Reply-To are not enough. Empty for every
-- provider that has no such handle (SMTP, Graph), which is why it is a plain
-- text default '' rather than nullable.
ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS thread_id text NOT NULL DEFAULT '';

-- Existing steps keep the behaviour the dashboard and the docs described until
-- now: "a follow-up threads on the previous step's subject, and changing that
-- subject starts a new thread instead". A step carrying a subject that is not
-- the conversation's was written to open a fresh one, so it keeps doing that;
-- everything else replies in the thread, which is what it always claimed to do
-- and never did.
--
-- The comparison is against the previous step that HAS a subject, not the
-- previous step: a blank follow-up in between inherits the conversation rather
-- than replacing it, so the step after it is still continuing the same one.
-- models.ThreadReplyDefaults applies the identical rule to steps created
-- through the API, so old and new campaigns answer the same way.
WITH named AS (
    SELECT id,
           btrim(subject) AS subject,
           LAG(btrim(subject)) OVER (
               PARTITION BY campaign_id ORDER BY position, created_at
           ) AS prev_subject
    FROM sequences
    WHERE kind = 'email' AND btrim(subject) <> ''
)
UPDATE sequences s
SET thread_reply = false
FROM named o
WHERE o.id = s.id
  AND o.prev_subject IS NOT NULL
  AND o.subject <> o.prev_subject;
