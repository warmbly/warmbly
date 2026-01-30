CREATE KEYSPACE IF NOT EXISTS maildata
WITH replication = {
    'class': 'SingleRegionStrategy'
};

USE maildata;

// EMAIL FOLDERS / MAILBOXES (ONLY IMAP)
CREATE TABLE IF NOT EXISTS mailboxes (
    user_id              text,
    email_id             text,
    mailbox              text,
    attributes           list<text>,
    uid_validity         bigint,
    highestmodseq        bigint,
    created_at           timestamp,

    PRIMARY KEY ((user_id, email_id), uid_validity)
);

// STORE EMAIL MESSAGES
CREATE TABLE emails (
    id                   text,
    user_id              uuid,
    email_id             uuid,
    mailbox              bigint,
    thread_id            text,
    message_id           text,
    gmail_id             text,
    parent_id            text,
    uid                  bigint,
    mod_seq              bigint,
    flags                set<text>,
    bcc                  list<text>,
    cc                   list<text>,
from_addr                list<text>,
    in_reply_to          list<text>,
    reply_to             list<text>,
    to_addr              list<text>,
    subject              text,
    size                 bigint,
    internal_date        timestamp,
    sent_date            timestamp,
    snippet              text,
    seen                 boolean,
    updated_at           timestamp,
    created_at           timestamp,

    PRIMARY KEY ((user_id), internal_date, id)
)
WITH CLUSTERING ORDER BY (internal_date DESC, id ASC)
AND default_time_to_live = 63072000; -- 2 years in seconds
    
CREATE INDEX emails_message_id_idx ON emails (id);
    
CREATE INDEX emails_message_msg_id_idx ON emails (message_id);
    
    // CREATE A MATERIALIZED VIEW TO BE ABLE TO QUERY BY Thread ID
    CREATE MATERIALIZED VIEW emails_by_thread AS
SELECT user_id, thread_id, email_id, id, subject, snippet, internal_date, seen
FROM emails
WHERE user_id IS NOT NULL AND email_id IS NOT NULL AND thread_id IS NOT NULL

    PRIMARY KEY ((user_id, email_id, thread_id), internal_date, id)
WITH CLUSTERING ORDER BY (internal_date DESC, id ASC);
    
    // CREATE A MATERIALIZED VIEW TO BE ABLE TO QUERY BY Email ID
    CREATE MATERIALIZED VIEW emails_by_email_id AS
SELECT *
FROM emails
WHERE user_id IS NOT NULL AND email_id IS NOT NULL

    PRIMARY KEY ((user_id, email_id), internal_date DESC, id)
WITH CLUSTERING ORDER BY (internal_date DESC, id ASC);

    // CREATE A DISTRIBUTED INDEX FOR ADDRESSES THAT I CAN USE TO QUERY BY
CREATE TABLE email_addrs (
    user_id                 uuid,
addr_type                   text, -- 'from', 'to', 'cc', 'bcc'
    addr                    text,
    internal_date           timestamp,
    email_message_id        uuid,

    PRIMARY KEY ((user_id, addr_type, addr), internal_date, email_message_id)
) WITH CLUSTERING ORDER BY (internal_date DESC, email_message_id ASC)
AND default_time_to_live = 63072000;

