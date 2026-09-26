-- Who hosts a contact's inbox (google_workspace, microsoft365, yahoo, ...), read
-- from the domain's MX by a backend sweep. esp_provider stays the coarse family
-- ESP matching reads. NOT VALID: every existing row holds the default.
ALTER TABLE contacts
    ADD COLUMN mail_host text NOT NULL DEFAULT '',
    ADD CONSTRAINT contacts_mail_host_check CHECK (mail_host ~ '^[a-z0-9_]{0,32}$') NOT VALID;
