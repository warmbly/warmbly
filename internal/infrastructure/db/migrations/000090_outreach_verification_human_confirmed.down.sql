ALTER TABLE outreach_contact_candidates
    DROP CONSTRAINT IF EXISTS outreach_contact_candidates_verification_check;

ALTER TABLE outreach_contact_candidates
    ADD CONSTRAINT outreach_contact_candidates_verification_check
        CHECK (verification_status IN (
            'OFFICIAL_SOURCE',
            'PUBLIC_DOCUMENT_RECENT',
            'MULTIPLE_PUBLIC_SOURCES',
            'INSTITUTIONAL_GENERIC',
            'PUBLIC_POSSIBLY_STALE',
            'CANDIDATE_UNVERIFIED',
            'NOT_FOUND',
            'INVALID',
            'BOUNCED',
            'DO_NOT_CONTACT'
        ));
