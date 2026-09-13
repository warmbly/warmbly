-- Retires the invalid-token abuse signal (#482). The only path that ever
-- recorded an attempt charged the mailbox that received a token, which it
-- never controlled, and was removed in #481; nothing has written here since,
-- the band that read it could not fire, and the admin tab that listed it could
-- only ever show history. A forged token has no attributable sender either,
-- so no future signal is waiting for the table. What is held against a
-- mailbox is what its owner does to warmup mail it received (deletion, spam
-- flag), which lives in warmup_spam_reports.
DROP TABLE IF EXISTS public.warmup_invalid_token_attempts;
