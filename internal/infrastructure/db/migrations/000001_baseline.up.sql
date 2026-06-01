-- ============================================================================
-- Squashed baseline schema (supersedes the original migrations 000001–000065).
--
-- This single migration is the exact cumulative result of those 65 migrations,
-- captured with `pg_dump --schema-only` against a freshly, fully-migrated
-- database and then stripped of ownership / privilege / psql-meta noise. It
-- produces a schema byte-for-byte identical to running the old chain.
--
-- Per-migration rationale (security posture, index intent, design notes) lives
-- in git history prior to this squash.
--
-- EXISTING DATABASES: a DB that already ran the old 1–65 chain must be reset
-- (`make db-wipe && make migrate && make seed`); golang-migrate cannot reconcile
-- a DB stamped at version 65 against a source whose highest version is 1. There
-- is no production data, so this is safe.
-- ============================================================================

-- Name: public; Type: SCHEMA; Schema: -; Owner: -
--

-- *not* creating schema, since initdb creates it

--
-- Name: activity_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.activity_type AS ENUM (
    'email_sent',
    'email_opened',
    'email_clicked',
    'email_replied',
    'email_bounced',
    'note_added',
    'note_updated',
    'deal_created',
    'deal_stage_changed',
    'deal_won',
    'deal_lost',
    'task_created',
    'task_completed',
    'contact_created',
    'contact_updated',
    'campaign_added',
    'campaign_removed'
);

--
-- Name: admin_outreach_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.admin_outreach_status AS ENUM (
    'queued',
    'sent',
    'failed'
);

--
-- Name: campaign_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.campaign_status AS ENUM (
    'draft',
    'active',
    'paused',
    'completed',
    'paused_trial_expired',
    'paused_no_accounts'
);

--
-- Name: conversation_generation_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.conversation_generation_status AS ENUM (
    'none',
    'slow',
    'medium',
    'fast',
    'ultra'
);

--
-- Name: conversation_theme_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.conversation_theme_status AS ENUM (
    'inactive',
    'active'
);

--
-- Name: crm_task_priority; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.crm_task_priority AS ENUM (
    'low',
    'medium',
    'high',
    'urgent'
);

--
-- Name: crm_task_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.crm_task_status AS ENUM (
    'pending',
    'in_progress',
    'completed',
    'cancelled'
);

--
-- Name: deal_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.deal_status AS ENUM (
    'open',
    'won',
    'lost'
);

--
-- Name: discount_code_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.discount_code_status AS ENUM (
    'active',
    'disabled',
    'expired'
);

--
-- Name: discount_duration; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.discount_duration AS ENUM (
    'once',
    'repeating',
    'forever'
);

--
-- Name: discount_redemption_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.discount_redemption_status AS ENUM (
    'pending',
    'applied',
    'canceled'
);

--
-- Name: discount_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.discount_type AS ENUM (
    'percent',
    'fixed',
    'trial_extension'
);

--
-- Name: email_error_resolve_method; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.email_error_resolve_method AS ENUM (
    'OAUTH',
    'RETRY',
    'RELOAD',
    'NONE'
);

--
-- Name: email_error_severity; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.email_error_severity AS ENUM (
    'CRITICAL',
    'WARNING',
    'INFORMATIONAL'
);

--
-- Name: email_provider; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.email_provider AS ENUM (
    'gmail',
    'outlook',
    'smtp_imap'
);

--
-- Name: email_risk_band; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.email_risk_band AS ENUM (
    'clean',
    'risky',
    'quarantine'
);

--
-- Name: email_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.email_status AS ENUM (
    'active',
    'inactive',
    'revoked'
);

--
-- Name: limit_request_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.limit_request_status AS ENUM (
    'pending',
    'approved',
    'rejected',
    'cancelled'
);

--
-- Name: release_channel; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.release_channel AS ENUM (
    'pinned',
    'stable',
    'dev'
);

--
-- Name: subscription_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.subscription_status AS ENUM (
    'trialing',
    'active',
    'past_due',
    'canceled',
    'unpaid',
    'incomplete',
    'incomplete_expired',
    'paused'
);

--
-- Name: task_status; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.task_status AS ENUM (
    'pending',
    'active',
    'completed',
    'failed',
    'cancelled',
    'skipped_trial_expired',
    'skipped_daily_limit',
    'skipped_suppressed',
    'skipped_no_warmup_access',
    'dead_lettered',
    'skipped_warmup_protected'
);

--
-- Name: task_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.task_type AS ENUM (
    'campaign',
    'warmup',
    'email'
);

--
-- Name: warmup_pool_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.warmup_pool_type AS ENUM (
    'free',
    'premium'
);

--
-- Name: worker_install_state; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.worker_install_state AS ENUM (
    'pending',
    'provisioning',
    'installed',
    'error',
    'uninstalling',
    'uninstalled'
);

--
-- Name: worker_risk_pool; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.worker_risk_pool AS ENUM (
    'clean',
    'risky',
    'quarantine'
);

--
-- Name: worker_type; Type: TYPE; Schema: public; Owner: -
--

CREATE TYPE public.worker_type AS ENUM (
    'shared',
    'dedicated'
);

--
-- Name: admin_audit_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admin_audit_logs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    admin_user_id uuid NOT NULL,
    action character varying(100) NOT NULL,
    target_type character varying(50) NOT NULL,
    target_id uuid NOT NULL,
    details jsonb,
    ip_address text,
    user_agent text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: admin_outreach_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.admin_outreach_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    sent_by uuid NOT NULL,
    to_user_id uuid,
    to_org_id uuid,
    to_email character varying(320) NOT NULL,
    reply_to character varying(320),
    subject text NOT NULL,
    body text NOT NULL,
    status public.admin_outreach_status DEFAULT 'queued'::public.admin_outreach_status NOT NULL,
    error text,
    sent_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: api_idempotency_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_idempotency_keys (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    key text NOT NULL,
    method text NOT NULL,
    path text NOT NULL,
    request_hash text NOT NULL,
    status text NOT NULL,
    status_code integer,
    response_body bytea,
    content_type text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT api_idempotency_keys_status_check CHECK ((status = ANY (ARRAY['processing'::text, 'completed'::text])))
);

--
-- Name: api_key_usage_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_key_usage_logs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    api_key_id uuid NOT NULL,
    endpoint character varying(255) NOT NULL,
    method character varying(10) NOT NULL,
    ip_address inet NOT NULL,
    user_agent text,
    response_status integer,
    response_time_ms integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.api_keys (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    key_prefix character varying(8) NOT NULL,
    key_hash text NOT NULL,
    permissions bigint DEFAULT 0 NOT NULL,
    allowed_ips text[],
    allowed_email_accounts uuid[],
    status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    last_used_at timestamp with time zone,
    expires_at timestamp with time zone,
    revoked_at timestamp with time zone,
    revoked_reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    key_suffix character varying(4) DEFAULT ''::character varying NOT NULL,
    rate_limit_per_minute integer DEFAULT 60 NOT NULL,
    description text,
    last_request_ip inet
);

--
-- Name: audit_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_logs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    actor_id uuid,
    action text NOT NULL,
    entity_type text NOT NULL,
    entity_id uuid,
    ip_address text DEFAULT ''::text NOT NULL,
    user_agent text DEFAULT ''::text NOT NULL,
    changes jsonb DEFAULT '{}'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: aws_credentials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.aws_credentials (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(120) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    region character varying(40) NOT NULL,
    access_key_id text NOT NULL,
    secret_access_key_encrypted text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: campaign_ab_assignments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_ab_assignments (
    campaign_id uuid NOT NULL,
    contact_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    assigned_at timestamp with time zone DEFAULT now() NOT NULL,
    opened_at timestamp with time zone,
    clicked_at timestamp with time zone,
    replied_at timestamp with time zone,
    bounced_at timestamp with time zone
);

--
-- Name: campaign_ab_variants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_ab_variants (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    campaign_id uuid NOT NULL,
    name text NOT NULL,
    weight integer DEFAULT 100 NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    body_html text DEFAULT ''::text NOT NULL,
    body_plain text DEFAULT ''::text NOT NULL,
    is_control boolean DEFAULT false NOT NULL,
    is_active boolean DEFAULT true NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT campaign_variant_weight_positive CHECK ((weight > 0))
);

--
-- Name: campaign_advanced_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_advanced_settings (
    campaign_id uuid NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: campaign_contact_progress; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_contact_progress (
    campaign_id uuid NOT NULL,
    contact_id uuid NOT NULL,
    sequence_id uuid NOT NULL,
    sent_at timestamp with time zone,
    opened_at timestamp with time zone,
    clicked_at timestamp with time zone,
    replied_at timestamp with time zone,
    bounced_at timestamp with time zone
);

--
-- Name: campaign_email_tags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_email_tags (
    tag_id uuid NOT NULL,
    campaign_id uuid NOT NULL
);

--
-- Name: campaign_folders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_folders (
    campaign_id uuid NOT NULL,
    folder_id uuid NOT NULL
);

--
-- Name: campaign_leads; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_leads (
    contact_id uuid NOT NULL,
    campaign_id uuid NOT NULL,
    "position" integer
);

--
-- Name: campaign_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_logs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    campaign_id uuid NOT NULL,
    event_type character varying(50) NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: campaign_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaign_tasks (
    task_id uuid NOT NULL,
    campaign_id uuid,
    contact_id uuid,
    sequence_id uuid
);

--
-- Name: campaigns; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.campaigns (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    organization_id uuid,
    name character varying(50) NOT NULL,
    description text NOT NULL,
    status public.campaign_status DEFAULT 'draft'::public.campaign_status NOT NULL,
    stop_on_reply boolean DEFAULT false NOT NULL,
    open_tracking boolean DEFAULT false NOT NULL,
    link_tracking boolean DEFAULT false NOT NULL,
    text_only boolean DEFAULT false NOT NULL,
    daily_limit integer DEFAULT 50 NOT NULL,
    unsubscribe_header boolean DEFAULT true NOT NULL,
    risky_emails boolean DEFAULT true NOT NULL,
    cc_addr text[] DEFAULT '{}'::text[] NOT NULL,
    bcc_addr text[] DEFAULT '{}'::text[] NOT NULL,
    start_date timestamp without time zone,
    end_date timestamp without time zone,
    timezone text DEFAULT 'Europe/London'::text NOT NULL,
    days smallint NOT NULL,
    start_time time without time zone DEFAULT '08:00:00'::time without time zone NOT NULL,
    end_time time without time zone DEFAULT '18:00:00'::time without time zone NOT NULL,
    last_status_change_at timestamp with time zone,
    updated_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone NOT NULL,
    contact_order_by character varying(20) DEFAULT 'created_at'::character varying,
    contact_order_dir character varying(4) DEFAULT 'asc'::character varying,
    contact_order_field text
);

--
-- Name: categories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.categories (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    title character varying(255) NOT NULL,
    color character varying(7) NOT NULL,
    "position" integer NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_color CHECK (((color)::text ~* '^#[a-f0-9]{6}$'::text))
);

--
-- Name: cloud_credentials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cloud_credentials (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    provider text NOT NULL,
    name text NOT NULL,
    encrypted_token text NOT NULL,
    last_used_at timestamp with time zone,
    last_test_at timestamp with time zone,
    last_test_ok boolean,
    last_test_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT cloud_credentials_provider_check CHECK ((provider = 'hetzner'::text))
);

--
-- Name: contact_activities; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.contact_activities (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    contact_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    user_id uuid,
    activity_type public.activity_type NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: contact_categories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.contact_categories (
    contact_id uuid NOT NULL,
    category_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);

--
-- Name: contact_notes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.contact_notes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    contact_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    content text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: contacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.contacts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    organization_id uuid,
    first_name text NOT NULL,
    last_name text NOT NULL,
    email text NOT NULL,
    company text NOT NULL,
    phone text NOT NULL,
    custom_fields jsonb NOT NULL,
    subscribed boolean DEFAULT true,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);

--
-- Name: conversation_messages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.conversation_messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conversation_id uuid NOT NULL,
    parent_id uuid,
    subject text NOT NULL
);

--
-- Name: conversation_themes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.conversation_themes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    status public.conversation_theme_status DEFAULT 'inactive'::public.conversation_theme_status NOT NULL,
    generation_status public.conversation_generation_status DEFAULT 'none'::public.conversation_generation_status NOT NULL
);

--
-- Name: conversations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.conversations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    theme uuid NOT NULL,
    language uuid NOT NULL,
    title text NOT NULL,
    description text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: crm_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.crm_tasks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    contact_id uuid,
    deal_id uuid,
    assigned_to uuid,
    created_by uuid NOT NULL,
    title character varying(255) NOT NULL,
    description text,
    due_date timestamp with time zone,
    priority public.crm_task_priority DEFAULT 'medium'::public.crm_task_priority NOT NULL,
    status public.crm_task_status DEFAULT 'pending'::public.crm_task_status NOT NULL,
    completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: daily_email_counts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.daily_email_counts (
    email_account_id uuid NOT NULL,
    date date NOT NULL,
    count integer DEFAULT 0 NOT NULL
);

--
-- Name: deals; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.deals (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    pipeline_id uuid NOT NULL,
    stage_id uuid NOT NULL,
    contact_id uuid,
    name character varying(255) NOT NULL,
    value numeric(12,2),
    currency character varying(3) DEFAULT 'USD'::character varying,
    status public.deal_status DEFAULT 'open'::public.deal_status NOT NULL,
    expected_close_date date,
    won_at timestamp with time zone,
    lost_at timestamp with time zone,
    lost_reason text,
    assigned_to uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: decision_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.decision_log (
    id bigint NOT NULL,
    kind text NOT NULL,
    worker_id uuid,
    mailbox_id uuid,
    before jsonb,
    after jsonb,
    reason text,
    triggered_by text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: decision_log_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.decision_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: decision_log_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.decision_log_id_seq OWNED BY public.decision_log.id;

--
-- Name: dedicated_worker_assignments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dedicated_worker_assignments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    worker_id uuid NOT NULL,
    user_id uuid NOT NULL,
    subscription_id uuid NOT NULL,
    assigned_at timestamp with time zone DEFAULT now() NOT NULL,
    released_at timestamp with time zone
);

--
-- Name: deliverability_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.deliverability_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    campaign_id uuid,
    task_id uuid,
    contact_id uuid,
    event_type character varying(32) NOT NULL,
    provider character varying(64) DEFAULT 'manual'::character varying NOT NULL,
    recipient_email text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    idempotency_key text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: discount_code_plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.discount_code_plans (
    discount_code_id uuid NOT NULL,
    plan_id uuid NOT NULL
);

--
-- Name: discount_codes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.discount_codes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    code character varying(64) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    type public.discount_type NOT NULL,
    percent_off smallint,
    amount_off numeric(10,2),
    currency character varying(3),
    trial_extension_days integer,
    duration public.discount_duration DEFAULT 'once'::public.discount_duration NOT NULL,
    duration_in_months integer,
    max_redemptions integer,
    times_redeemed integer DEFAULT 0 NOT NULL,
    per_account_limit integer DEFAULT 1 NOT NULL,
    applies_to_all_plans boolean DEFAULT true NOT NULL,
    status public.discount_code_status DEFAULT 'active'::public.discount_code_status NOT NULL,
    starts_at timestamp with time zone,
    expires_at timestamp with time zone,
    created_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT discount_amount_positive CHECK (((amount_off IS NULL) OR (amount_off > (0)::numeric))),
    CONSTRAINT discount_max_redemptions_positive CHECK (((max_redemptions IS NULL) OR (max_redemptions > 0))),
    CONSTRAINT discount_per_account_positive CHECK ((per_account_limit >= 1)),
    CONSTRAINT discount_percent_bounds CHECK (((percent_off IS NULL) OR ((percent_off >= 1) AND (percent_off <= 100)))),
    CONSTRAINT discount_repeating_months CHECK (((duration <> 'repeating'::public.discount_duration) OR (duration_in_months IS NOT NULL))),
    CONSTRAINT discount_trial_positive CHECK (((trial_extension_days IS NULL) OR (trial_extension_days > 0))),
    CONSTRAINT discount_value_matches_type CHECK ((((type = 'percent'::public.discount_type) AND (percent_off IS NOT NULL) AND (amount_off IS NULL) AND (trial_extension_days IS NULL)) OR ((type = 'fixed'::public.discount_type) AND (amount_off IS NOT NULL) AND (currency IS NOT NULL) AND (percent_off IS NULL) AND (trial_extension_days IS NULL)) OR ((type = 'trial_extension'::public.discount_type) AND (trial_extension_days IS NOT NULL) AND (percent_off IS NULL) AND (amount_off IS NULL))))
);

--
-- Name: discount_redemptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.discount_redemptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    discount_code_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    redeemed_by uuid,
    subscription_id uuid,
    plan_id uuid,
    stripe_coupon_id character varying(255),
    stripe_checkout_session_id character varying(255),
    type public.discount_type NOT NULL,
    percent_off smallint,
    amount_off numeric(10,2),
    currency character varying(3),
    trial_extension_days integer,
    status public.discount_redemption_status DEFAULT 'pending'::public.discount_redemption_status NOT NULL,
    redeemed_at timestamp with time zone DEFAULT now() NOT NULL,
    applied_at timestamp with time zone
);

--
-- Name: durations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.durations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    title character varying(100) NOT NULL
);

--
-- Name: email_account_errors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_account_errors (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email_account_id uuid NOT NULL,
    user_id uuid NOT NULL,
    error_code character varying(100) NOT NULL,
    severity public.email_error_severity NOT NULL,
    resolve_method public.email_error_resolve_method DEFAULT 'NONE'::public.email_error_resolve_method NOT NULL,
    title character varying(255) NOT NULL,
    message text NOT NULL,
    user_message text,
    action_required text,
    task_id uuid,
    resolved_at timestamp with time zone,
    resolved_by character varying(100),
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: email_accounts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_accounts (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    organization_id uuid,
    worker_id uuid,
    email character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    signature_plain text NOT NULL,
    signature_html text NOT NULL,
    signature_sync boolean DEFAULT true NOT NULL,
    signature_code boolean DEFAULT false NOT NULL,
    provider public.email_provider NOT NULL,
    status public.email_status DEFAULT 'active'::public.email_status NOT NULL,
    last_synced_at timestamp without time zone,
    last_id bigint,
    campaign_limit integer DEFAULT 50 NOT NULL,
    min_wait_time integer DEFAULT 600 NOT NULL,
    reply_to text DEFAULT ''::text NOT NULL,
    tracking_domain text DEFAULT ''::text NOT NULL,
    timezone text DEFAULT 'UTC'::text NOT NULL,
    warmup timestamp without time zone,
    warmup_base integer DEFAULT 10 NOT NULL,
    warmup_max integer DEFAULT 40 NOT NULL,
    warmup_increase integer DEFAULT 1 NOT NULL,
    warmup_reply_rate smallint DEFAULT 30 NOT NULL,
    warmup_tag text NOT NULL,
    warmup_start_time time without time zone DEFAULT '08:00:00'::time without time zone NOT NULL,
    warmup_end_time time without time zone DEFAULT '20:00:00'::time without time zone NOT NULL,
    warmup_days smallint DEFAULT 0 NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    warmup_pool_type character varying(20) DEFAULT 'free'::character varying,
    risk_band public.email_risk_band DEFAULT 'clean'::public.email_risk_band NOT NULL,
    risk_evaluated_at timestamp with time zone,
    tracking_domain_verified boolean DEFAULT false NOT NULL,
    tracking_domain_verified_at timestamp with time zone,
    warmup_paused_at timestamp with time zone,
    CONSTRAINT valid_reply_rate CHECK (((warmup_reply_rate >= 0) AND (warmup_reply_rate <= 100)))
);

--
-- Name: email_accounts_oauth; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_accounts_oauth (
    email_account_id uuid NOT NULL,
    access_token text NOT NULL,
    refresh_token text NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);

--
-- Name: email_accounts_smtp_imap; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_accounts_smtp_imap (
    email_account_id uuid NOT NULL,
    smtp_host character varying(255) NOT NULL,
    smtp_port integer NOT NULL,
    smtp_user character varying(255) NOT NULL,
    smtp_password character varying(255) NOT NULL,
    imap_host character varying(255) NOT NULL,
    imap_port integer NOT NULL,
    imap_user character varying(255) NOT NULL,
    imap_password character varying(255) NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL
);

--
-- Name: email_tags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_tags (
    email_id uuid NOT NULL,
    tag_id uuid NOT NULL
);

--
-- Name: email_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_tasks (
    task_id uuid NOT NULL,
    to_addrs text[],
    cc text[],
    bcc text[],
    in_reply_to text[],
    subject text NOT NULL,
    body text NOT NULL,
    body_html text DEFAULT ''::text NOT NULL,
    body_plain text DEFAULT ''::text NOT NULL,
    thread_id text,
    send_mode character varying(20) DEFAULT 'instant'::character varying NOT NULL,
    encrypted boolean DEFAULT true NOT NULL
);

--
-- Name: enterprise_inquiries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.enterprise_inquiries (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    company_name character varying(255) NOT NULL,
    contact_name character varying(255) NOT NULL,
    contact_email character varying(255) NOT NULL,
    estimated_volume integer,
    team_size integer,
    notes text,
    status character varying(50) DEFAULT 'pending'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    processed_at timestamp with time zone,
    processed_by uuid,
    user_id uuid,
    phone character varying(50),
    monthly_email_volume character varying(100),
    message text,
    assigned_to uuid,
    updated_at timestamp with time zone DEFAULT now()
);

--
-- Name: folders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.folders (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    title character varying(255) NOT NULL,
    color character varying(7) NOT NULL,
    "position" integer NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_color CHECK (((color)::text ~* '^#[a-f0-9]{6}$'::text))
);

--
-- Name: integration_connections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.integration_connections (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    provider text NOT NULL,
    label text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    inbound_secret text,
    config_encrypted bytea,
    display_fields jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_synced_at timestamp with time zone,
    last_error text,
    last_error_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    connected_by_user_id uuid,
    auth_method text DEFAULT 'api_key'::text NOT NULL,
    access_token_encrypted text,
    refresh_token_encrypted text,
    token_expires_at timestamp with time zone,
    granted_scopes text[] DEFAULT '{}'::text[] NOT NULL,
    external_account_id text,
    external_account_name text,
    health text DEFAULT 'unknown'::text NOT NULL,
    health_detail text,
    health_checked_at timestamp with time zone,
    CONSTRAINT integration_connections_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'authorizing'::text, 'connected'::text, 'degraded'::text, 'reauth_required'::text, 'disconnected'::text])))
);

--
-- Name: integration_event_subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.integration_event_subscriptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    connection_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    event_type text NOT NULL,
    action text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: integration_field_mappings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.integration_field_mappings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    connection_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    direction text DEFAULT 'push'::text NOT NULL,
    warmbly_field text NOT NULL,
    external_field text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT integration_field_mappings_direction_check CHECK ((direction = ANY (ARRAY['push'::text, 'pull'::text, 'both'::text])))
);

--
-- Name: integration_oauth_states; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.integration_oauth_states (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    provider text NOT NULL,
    state text NOT NULL,
    code_verifier text DEFAULT ''::text NOT NULL,
    label text DEFAULT ''::text NOT NULL,
    requested_scopes text[] DEFAULT '{}'::text[] NOT NULL,
    used_at timestamp with time zone,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: integration_sync_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.integration_sync_runs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    connection_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    kind text NOT NULL,
    status text DEFAULT 'running'::text NOT NULL,
    detail text DEFAULT ''::text NOT NULL,
    records_processed integer DEFAULT 0 NOT NULL,
    started_at timestamp with time zone DEFAULT now() NOT NULL,
    finished_at timestamp with time zone,
    CONSTRAINT integration_sync_runs_status_check CHECK ((status = ANY (ARRAY['running'::text, 'success'::text, 'error'::text])))
);

--
-- Name: languages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.languages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL
);

--
-- Name: limit_increase_requests; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.limit_increase_requests (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    field character varying(64) NOT NULL,
    current_effective integer NOT NULL,
    requested integer NOT NULL,
    reason text NOT NULL,
    status public.limit_request_status DEFAULT 'pending'::public.limit_request_status NOT NULL,
    submitted_by uuid NOT NULL,
    submitted_at timestamp with time zone DEFAULT now() NOT NULL,
    reviewed_by uuid,
    reviewed_at timestamp with time zone,
    review_notes text DEFAULT ''::text NOT NULL,
    CONSTRAINT requested_larger_than_current CHECK ((requested > current_effective)),
    CONSTRAINT requested_positive CHECK ((requested > 0))
);

--
-- Name: meeting_bookings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.meeting_bookings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    source text NOT NULL,
    external_event_id text NOT NULL,
    invitee_email text NOT NULL,
    invitee_name text DEFAULT ''::text NOT NULL,
    event_name text DEFAULT ''::text NOT NULL,
    scheduled_for timestamp with time zone,
    contact_id uuid,
    campaign_id uuid,
    raw_payload jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: offer_options; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.offer_options (
    offer_id uuid NOT NULL,
    plan_id uuid NOT NULL,
    title character varying(255) NOT NULL
);

--
-- Name: offers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.offers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    title character varying(255) NOT NULL,
    description text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: organization_invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.organization_invitations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    email character varying(255) NOT NULL,
    role character varying(50) DEFAULT 'viewer'::character varying NOT NULL,
    permissions smallint DEFAULT 0 NOT NULL,
    invited_by uuid NOT NULL,
    token character varying(64) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: organization_limit_overrides; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.organization_limit_overrides (
    organization_id uuid NOT NULL,
    max_campaigns integer DEFAULT 0 NOT NULL,
    max_active_campaigns integer DEFAULT 0 NOT NULL,
    max_team_members integer DEFAULT 0 NOT NULL,
    max_email_accounts integer DEFAULT 0 NOT NULL,
    max_contacts integer DEFAULT 0 NOT NULL,
    daily_campaign_limit integer DEFAULT 0 NOT NULL,
    granted_by uuid,
    granted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    notes text DEFAULT ''::text NOT NULL,
    CONSTRAINT non_negative_overrides CHECK (((max_campaigns >= 0) AND (max_active_campaigns >= 0) AND (max_team_members >= 0) AND (max_email_accounts >= 0) AND (max_contacts >= 0) AND (daily_campaign_limit >= 0)))
);

--
-- Name: organization_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.organization_members (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role character varying(50) DEFAULT 'viewer'::character varying NOT NULL,
    permissions smallint DEFAULT 0 NOT NULL,
    invited_by uuid,
    invited_at timestamp with time zone DEFAULT now() NOT NULL,
    accepted_at timestamp with time zone
);

--
-- Name: organizations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.organizations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(255) NOT NULL,
    slug character varying(100),
    owner_user_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    avatar_url text,
    deletion_scheduled_at timestamp with time zone,
    deletion_scheduled_for timestamp with time zone
);

--
-- Name: outreach_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.outreach_settings (
    organization_id uuid NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    updated_by uuid,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: pipeline_stages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pipeline_stages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    pipeline_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    color character varying(7) DEFAULT '#6366f1'::character varying NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT pipeline_stages_color_check CHECK (((color)::text ~* '^#[a-f0-9]{6}$'::text))
);

--
-- Name: pipelines; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pipelines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: plan_rate_limits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plan_rate_limits (
    plan_id uuid NOT NULL,
    limit_read_pm integer DEFAULT 6000 NOT NULL,
    limit_write_pm integer DEFAULT 6000 NOT NULL,
    limit_bulk_pm integer DEFAULT 600 NOT NULL,
    limit_unibox_pm integer DEFAULT 1200 NOT NULL,
    limit_analytics_pm integer DEFAULT 600 NOT NULL,
    limit_api_calls_daily integer DEFAULT 500000 NOT NULL,
    limit_bulk_ops_daily integer DEFAULT 100 NOT NULL,
    limit_ws_message_pm integer DEFAULT 120 NOT NULL,
    limit_ws_join_pm integer DEFAULT 30 NOT NULL,
    limit_ws_event_pm integer DEFAULT 60 NOT NULL,
    max_connections integer DEFAULT 10 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(100),
    max_contacts bigint NOT NULL,
    daily_emails integer NOT NULL,
    ai_generation boolean NOT NULL,
    account_limit integer NOT NULL,
    price numeric(10,2) NOT NULL,
    discounted_price numeric(10,2) NOT NULL,
    duration_id uuid NOT NULL,
    savings smallint,
    public boolean DEFAULT false NOT NULL,
    stripe_price_id character varying(255),
    stripe_product_id character varying(255),
    max_campaigns integer,
    max_active_campaigns integer,
    max_team_members integer,
    max_email_accounts integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    dedicated_workers integer DEFAULT 0 NOT NULL,
    daily_campaign_limit integer,
    stripe_price_id_yearly character varying(255),
    CONSTRAINT valid_savings CHECK (((savings >= 0) AND (savings <= 100)))
);

--
-- Name: platform_statistics; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.platform_statistics (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    stat_date date NOT NULL,
    total_users integer DEFAULT 0 NOT NULL,
    active_users integer DEFAULT 0 NOT NULL,
    total_emails_sent integer DEFAULT 0 NOT NULL,
    total_campaigns integer DEFAULT 0 NOT NULL,
    active_campaigns integer DEFAULT 0 NOT NULL,
    total_workers integer DEFAULT 0 NOT NULL,
    active_workers integer DEFAULT 0 NOT NULL,
    warmup_blocked_count integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: preflight_reports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.preflight_reports (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    campaign_id uuid NOT NULL,
    passed boolean NOT NULL,
    score integer NOT NULL,
    checks jsonb DEFAULT '[]'::jsonb NOT NULL,
    recommendations jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: provisioning_jobs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provisioning_jobs (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    triggered_by text NOT NULL,
    provider text NOT NULL,
    credential_id uuid,
    template_id uuid,
    config jsonb NOT NULL,
    provider_server_id text,
    provider_ip_ids text[],
    ips inet[],
    worker_ids uuid[],
    est_monthly_cost numeric(10,2),
    cost_currency text DEFAULT 'EUR'::text,
    error text,
    attempts integer DEFAULT 0 NOT NULL,
    last_step_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT provisioning_jobs_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'creating_server'::text, 'creating_ips'::text, 'assigning_ips'::text, 'setting_rdns'::text, 'installing'::text, 'verifying'::text, 'completed'::text, 'failed'::text, 'rolling_back'::text])))
);

--
-- Name: provisioning_policy; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provisioning_policy (
    provider text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    auto_provision boolean DEFAULT false NOT NULL,
    max_per_day integer DEFAULT 2 NOT NULL,
    max_per_month integer DEFAULT 30 NOT NULL,
    monthly_budget numeric(10,2) DEFAULT 500,
    budget_currency text DEFAULT 'EUR'::text,
    cooldown_min integer DEFAULT 60 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: provisioning_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.provisioning_templates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name text NOT NULL,
    description text,
    provider text NOT NULL,
    location text NOT NULL,
    datacenter text,
    server_type text NOT NULL,
    image text DEFAULT 'ubuntu-22.04'::text NOT NULL,
    server_count integer DEFAULT 1 NOT NULL,
    ipv4_per_server integer DEFAULT 1 NOT NULL,
    ipv6_per_server integer DEFAULT 1 NOT NULL,
    worker_profile_id uuid,
    tier text NOT NULL,
    egress_kind text DEFAULT 'cold_smtp'::text NOT NULL,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    placement_group text,
    private_network text,
    firewall text,
    is_auto_template boolean DEFAULT false NOT NULL,
    est_monthly_cost numeric(10,2),
    est_cost_currency text DEFAULT 'EUR'::text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT provisioning_templates_egress_kind_check CHECK ((egress_kind = ANY (ARRAY['cold_smtp'::text, 'oauth_api'::text, 'warmup_only'::text]))),
    CONSTRAINT provisioning_templates_ipv4_per_server_check CHECK (((ipv4_per_server >= 1) AND (ipv4_per_server <= 64))),
    CONSTRAINT provisioning_templates_server_count_check CHECK (((server_count >= 1) AND (server_count <= 100))),
    CONSTRAINT provisioning_templates_tier_check CHECK ((tier = ANY (ARRAY['shared_free'::text, 'shared_premium'::text, 'dedicated'::text])))
);

--
-- Name: realtime_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.realtime_events (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    org_id uuid,
    event_type character varying(50) NOT NULL,
    priority character varying(10) DEFAULT 'normal'::character varying NOT NULL,
    payload jsonb NOT NULL,
    delivered boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone DEFAULT (now() + '24:00:00'::interval) NOT NULL
);

--
-- Name: reply_intents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.reply_intents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    contact_email text NOT NULL,
    campaign_id uuid,
    task_id uuid,
    intent character varying(32) NOT NULL,
    confidence numeric(5,2) DEFAULT 0 NOT NULL,
    action_taken text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: reply_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.reply_templates (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    name character varying(255) NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    body_html text DEFAULT ''::text NOT NULL,
    body_plain text DEFAULT ''::text NOT NULL,
    "position" integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.roles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    permissions bigint NOT NULL,
    name character varying(255) NOT NULL,
    color character varying(7) NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_color CHECK (((color)::text ~* '^#[a-f0-9]{6}$'::text))
);

--
-- Name: scheduled_deletions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scheduled_deletions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    resource_type character varying(32) NOT NULL,
    resource_id uuid NOT NULL,
    organization_id uuid,
    requested_by_user_id uuid NOT NULL,
    reason text,
    scheduled_at timestamp with time zone DEFAULT now() NOT NULL,
    execute_after timestamp with time zone NOT NULL,
    grace_days integer NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    cancelled_at timestamp with time zone,
    cancelled_by_user_id uuid,
    cancelled_reason text,
    executed_at timestamp with time zone,
    execution_error text,
    notifications_sent integer DEFAULT 0 NOT NULL,
    last_reminder_at timestamp with time zone
);

--
-- Name: secret_plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.secret_plans (
    user_id uuid NOT NULL,
    plan_id uuid NOT NULL
);

--
-- Name: sequences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sequences (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    campaign_id uuid NOT NULL,
    organization_id uuid,
    name character varying(50) NOT NULL,
    subject text NOT NULL,
    body_plain text NOT NULL,
    body_html text NOT NULL,
    body_sync boolean DEFAULT true NOT NULL,
    body_code boolean DEFAULT false NOT NULL,
    wait_after integer DEFAULT 10 NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    "position" integer DEFAULT 0 NOT NULL
);

--
-- Name: sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    current_organization_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    revoked_at timestamp with time zone,
    last_refreshed_at timestamp with time zone,
    refresh_nonce text,
    access_nonce text,
    location_city text,
    location_region text,
    location_country text,
    location_country_code text,
    location_postal_code text,
    os_name text,
    browser_name text,
    auth_provider text DEFAULT ''::text NOT NULL
);

--
-- Name: storage_backends; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.storage_backends (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    kind text NOT NULL,
    provider text NOT NULL,
    name text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    is_active boolean DEFAULT false NOT NULL,
    is_readonly boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT storage_backends_kind_check CHECK ((kind = ANY (ARRAY['kms'::text, 'encrypted_keys'::text, 'blob'::text, 'eventbus'::text, 'cache'::text])))
);

--
-- Name: stripe_webhook_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.stripe_webhook_events (
    id character varying(255) NOT NULL,
    event_type character varying(100) NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL,
    payload jsonb
);

--
-- Name: subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscriptions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    plan_id uuid NOT NULL,
    stripe_customer_id character varying(255) NOT NULL,
    stripe_subscription_id character varying(255),
    stripe_price_id character varying(255),
    status public.subscription_status DEFAULT 'incomplete'::public.subscription_status NOT NULL,
    current_period_start timestamp with time zone,
    current_period_end timestamp with time zone,
    cancel_at_period_end boolean DEFAULT false NOT NULL,
    canceled_at timestamp with time zone,
    trial_start timestamp with time zone,
    trial_end timestamp with time zone,
    is_enterprise boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    free_trial_started_at timestamp with time zone,
    free_trial_ends_at timestamp with time zone
);

--
-- Name: suppressed_recipients; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.suppressed_recipients (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    email text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    source character varying(32) NOT NULL,
    campaign_id uuid,
    expires_at timestamp with time zone,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: tags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    title character varying(255) NOT NULL,
    color character varying(7) NOT NULL,
    "position" integer NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_color CHECK (((color)::text ~* '^#[a-f0-9]{6}$'::text))
);

--
-- Name: task_dead_letters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_dead_letters (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    task_type character varying(32) NOT NULL,
    payload jsonb DEFAULT '{}'::jsonb NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 5 NOT NULL,
    status character varying(32) DEFAULT 'pending'::character varying NOT NULL,
    next_retry_at timestamp with time zone,
    replayed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: task_execution_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_execution_keys (
    task_id uuid NOT NULL,
    execution_key text NOT NULL,
    first_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL,
    attempts integer DEFAULT 1 NOT NULL,
    status character varying(32) DEFAULT 'in_progress'::character varying NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL
);

--
-- Name: task_failures; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.task_failures (
    task_id uuid NOT NULL,
    title text NOT NULL,
    message text NOT NULL
);

--
-- Name: tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tasks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    task_type public.task_type NOT NULL,
    email_account_id uuid NOT NULL,
    status public.task_status DEFAULT 'pending'::public.task_status NOT NULL,
    message_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    scheduled_at timestamp with time zone,
    completed_at timestamp with time zone,
    cloud_task_name text
);

--
-- Name: tracking_events_processed; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tracking_events_processed (
    task_id uuid NOT NULL,
    event_type character varying(20) NOT NULL,
    ip_hash character varying(32),
    url_hash character varying(16) DEFAULT ''::character varying NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: unibox_emails; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.unibox_emails (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    email_id uuid NOT NULL,
    mailbox integer DEFAULT 0 NOT NULL,
    thread_id text DEFAULT ''::text NOT NULL,
    message_id text DEFAULT ''::text NOT NULL,
    gmail_id text DEFAULT ''::text NOT NULL,
    parent_id text DEFAULT ''::text NOT NULL,
    uid integer DEFAULT 0 NOT NULL,
    mod_seq bigint DEFAULT 0 NOT NULL,
    flags text[] DEFAULT '{}'::text[] NOT NULL,
    bcc text[] DEFAULT '{}'::text[] NOT NULL,
    cc text[] DEFAULT '{}'::text[] NOT NULL,
    from_addr text[] DEFAULT '{}'::text[] NOT NULL,
    in_reply_to text[] DEFAULT '{}'::text[] NOT NULL,
    reply_to text[] DEFAULT '{}'::text[] NOT NULL,
    to_addr text[] DEFAULT '{}'::text[] NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    size bigint DEFAULT 0 NOT NULL,
    internal_date timestamp with time zone DEFAULT now() NOT NULL,
    sent_date timestamp with time zone DEFAULT now() NOT NULL,
    snippet text DEFAULT ''::text NOT NULL,
    seen boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    search_tsv tsvector GENERATED ALWAYS AS (to_tsvector('english'::regconfig, ((COALESCE(subject, ''::text) || ' '::text) || COALESCE(snippet, ''::text)))) STORED
);

--
-- Name: unibox_mailboxes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.unibox_mailboxes (
    email_id uuid NOT NULL,
    uid_validity integer NOT NULL,
    mailbox text DEFAULT ''::text NOT NULL,
    attributes text[] DEFAULT '{}'::text[] NOT NULL,
    highestmodseq bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: unibox_snoozes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.unibox_snoozes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    thread_id text NOT NULL,
    snoozed_until timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: user_bans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_bans (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    banned_by uuid NOT NULL,
    reason text NOT NULL,
    banned_at timestamp with time zone DEFAULT now() NOT NULL,
    unbanned_at timestamp with time zone,
    unbanned_by uuid,
    unban_reason text
);

--
-- Name: user_encrypted_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_encrypted_keys (
    user_id uuid NOT NULL,
    encrypted_data_key text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: user_rate_limits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_rate_limits (
    user_id uuid NOT NULL,
    limit_read_pm integer DEFAULT 6000 NOT NULL,
    limit_write_pm integer DEFAULT 6000 NOT NULL,
    limit_bulk_pm integer DEFAULT 600 NOT NULL,
    limit_unibox_pm integer DEFAULT 1200 NOT NULL,
    limit_analytics_pm integer DEFAULT 600 NOT NULL,
    limit_api_calls_daily integer DEFAULT 500000 NOT NULL,
    limit_bulk_ops_daily integer DEFAULT 100 NOT NULL,
    limit_ws_message_pm integer DEFAULT 120 NOT NULL,
    limit_ws_join_pm integer DEFAULT 30 NOT NULL,
    limit_ws_event_pm integer DEFAULT 60 NOT NULL,
    max_connections integer DEFAULT 10 NOT NULL,
    notes text,
    updated_by uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: user_roles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_roles (
    user_id uuid NOT NULL,
    role_id uuid NOT NULL
);

--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    first_name character varying(255) NOT NULL,
    last_name character varying(255) NOT NULL,
    email text NOT NULL,
    password_hash text,
    max_organizations integer DEFAULT 5 NOT NULL,
    free_trial_used boolean DEFAULT false NOT NULL,
    admin_permissions integer DEFAULT 0 NOT NULL,
    admin_granted_at timestamp with time zone,
    admin_granted_by uuid,
    banned_at timestamp with time zone,
    referral_source character varying(50),
    onboarding_completed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    avatar_url text,
    deletion_scheduled_at timestamp with time zone,
    deletion_scheduled_for timestamp with time zone,
    ban_scope integer DEFAULT 0 NOT NULL,
    job_role character varying(50),
    team_size character varying(20),
    CONSTRAINT ban_scope_non_negative CHECK ((ban_scope >= 0))
);

--
-- Name: warmup_admin_actions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_admin_actions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    admin_user_id uuid NOT NULL,
    email_account_id uuid NOT NULL,
    action character varying(50) NOT NULL,
    reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: warmup_appeals; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_appeals (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email_account_id uuid NOT NULL,
    user_id uuid NOT NULL,
    reason text NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    reviewed_by uuid,
    reviewed_at timestamp with time zone,
    review_notes text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: warmup_invalid_token_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_invalid_token_attempts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email_account_id uuid NOT NULL,
    attempted_token text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: warmup_pool_participants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_pool_participants (
    pool_id uuid NOT NULL,
    email_account_id uuid NOT NULL,
    joined_at timestamp with time zone DEFAULT now() NOT NULL,
    blocked_at timestamp with time zone,
    blocked_reason text,
    spam_score integer DEFAULT 0 NOT NULL,
    blocked_until timestamp with time zone,
    health_state character varying(20) DEFAULT 'healthy'::character varying NOT NULL,
    last_health_score double precision DEFAULT 0 NOT NULL,
    last_health_reason text,
    last_health_evaluated_at timestamp with time zone,
    participant_role text DEFAULT 'sender_receiver'::text NOT NULL,
    CONSTRAINT valid_spam_score CHECK (((spam_score >= 0) AND (spam_score <= 100))),
    CONSTRAINT warmup_pool_participants_health_state_check CHECK (((health_state)::text = ANY (ARRAY[('healthy'::character varying)::text, ('watch'::character varying)::text, ('throttled'::character varying)::text, ('quarantined'::character varying)::text, ('blocked'::character varying)::text]))),
    CONSTRAINT warmup_pool_participants_role_check CHECK ((participant_role = ANY (ARRAY['sender_receiver'::text, 'recipient_only'::text])))
);

--
-- Name: warmup_pools; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_pools (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    pool_type public.warmup_pool_type NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    max_participants integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: warmup_routing_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_routing_rules (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    name text NOT NULL,
    priority integer DEFAULT 100 NOT NULL,
    sender_match_type text NOT NULL,
    sender_match_value text DEFAULT ''::text NOT NULL,
    recipient_match_type text NOT NULL,
    recipient_match_value text DEFAULT ''::text NOT NULL,
    weight real DEFAULT 1.0 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT warmup_routing_rules_recipient_match_type_check CHECK ((recipient_match_type = ANY (ARRAY['any'::text, 'domain'::text, 'tld'::text, 'provider'::text]))),
    CONSTRAINT warmup_routing_rules_sender_match_type_check CHECK ((sender_match_type = ANY (ARRAY['any'::text, 'domain'::text, 'tld'::text, 'provider'::text]))),
    CONSTRAINT warmup_routing_rules_weight_check CHECK ((weight >= (0)::double precision))
);

--
-- Name: warmup_spam_reports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_spam_reports (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    reporter_account_id uuid NOT NULL,
    reported_account_id uuid NOT NULL,
    message_id text NOT NULL,
    report_type character varying(50) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: warmup_statistics; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_statistics (
    email_account_id uuid NOT NULL,
    date date NOT NULL,
    emails_sent integer DEFAULT 0 NOT NULL,
    emails_replied integer DEFAULT 0 NOT NULL,
    target_volume integer NOT NULL
);

--
-- Name: warmup_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_tasks (
    task_id uuid NOT NULL,
    target_account_id uuid
);

--
-- Name: warmup_tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.warmup_tokens (
    token uuid DEFAULT gen_random_uuid() NOT NULL,
    task_id uuid NOT NULL,
    sender_account_id uuid NOT NULL,
    recipient_account_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    consumed_at timestamp with time zone,
    expires_at timestamp with time zone DEFAULT (now() + '7 days'::interval) NOT NULL,
    conversation_theme text DEFAULT ''::text NOT NULL
);

--
-- Name: webauthn_credentials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webauthn_credentials (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    credential_id bytea NOT NULL,
    public_key bytea NOT NULL,
    attestation_type text DEFAULT ''::text NOT NULL,
    attestation_format text DEFAULT ''::text NOT NULL,
    transports jsonb DEFAULT '[]'::jsonb NOT NULL,
    aaguid bytea DEFAULT '\x'::bytea NOT NULL,
    sign_count bigint DEFAULT 0 NOT NULL,
    clone_warning boolean DEFAULT false NOT NULL,
    backup_eligible boolean DEFAULT false NOT NULL,
    backup_state boolean DEFAULT false NOT NULL,
    user_present boolean DEFAULT false NOT NULL,
    user_verified boolean DEFAULT false NOT NULL,
    name text DEFAULT 'Passkey'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_used_at timestamp with time zone
);

--
-- Name: webhook_deliveries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhook_deliveries (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    endpoint_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    event_type text NOT NULL,
    event_id uuid NOT NULL,
    payload jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 8 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    last_attempt_at timestamp with time zone,
    response_status integer,
    response_body_excerpt text,
    error_reason text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT webhook_deliveries_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'in_flight'::text, 'delivered'::text, 'failed'::text, 'abandoned'::text])))
);

--
-- Name: webhook_endpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhook_endpoints (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id uuid NOT NULL,
    url text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    secret text NOT NULL,
    event_types text[] DEFAULT '{}'::text[] NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    last_success_at timestamp with time zone,
    last_failure_at timestamp with time zone,
    last_failure_reason text,
    consecutive_failures integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

--
-- Name: worker_health_samples; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.worker_health_samples (
    id bigint NOT NULL,
    worker_id uuid NOT NULL,
    observed_at timestamp with time zone DEFAULT now() NOT NULL,
    assigned_count integer DEFAULT 0 NOT NULL,
    imap_idle_count integer DEFAULT 0 NOT NULL,
    memory_mb integer DEFAULT 0 NOT NULL,
    goroutine_count integer DEFAULT 0 NOT NULL,
    sends_attempted integer DEFAULT 0 NOT NULL,
    sends_succeeded integer DEFAULT 0 NOT NULL,
    bounces_hard integer DEFAULT 0 NOT NULL,
    bounces_soft integer DEFAULT 0 NOT NULL,
    complaints integer DEFAULT 0 NOT NULL,
    auth_errors integer DEFAULT 0 NOT NULL,
    rate_limit_errors integer DEFAULT 0 NOT NULL,
    smtp_latency_p50_ms integer DEFAULT 0 NOT NULL,
    smtp_latency_p99_ms integer DEFAULT 0 NOT NULL
);

--
-- Name: workers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(255) DEFAULT ''::character varying NOT NULL,
    notes text,
    ip_addr text NOT NULL,
    active boolean DEFAULT false,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    worker_type public.worker_type DEFAULT 'shared'::public.worker_type NOT NULL,
    account_count integer DEFAULT 0 NOT NULL,
    free_tier boolean DEFAULT true NOT NULL,
    ssh_host text,
    ssh_port integer DEFAULT 22 NOT NULL,
    ssh_user character varying(64) DEFAULT 'root'::character varying NOT NULL,
    ssh_public_key text,
    ssh_private_key_encrypted text,
    ssh_host_fingerprint text,
    install_state public.worker_install_state DEFAULT 'pending'::public.worker_install_state NOT NULL,
    last_seen_at timestamp with time zone,
    last_error text,
    enrollment_token_hash text,
    enrollment_token_expires_at timestamp with time zone,
    profile_id uuid,
    config_applied_at timestamp with time zone,
    image_version text DEFAULT ''::text NOT NULL,
    risk_pool public.worker_risk_pool DEFAULT 'clean'::public.worker_risk_pool NOT NULL,
    egress_kind text DEFAULT 'cold_smtp'::text NOT NULL,
    health_state text DEFAULT 'healthy'::text NOT NULL,
    load_score numeric(10,2) DEFAULT 0 NOT NULL,
    CONSTRAINT workers_egress_kind_check CHECK ((egress_kind = ANY (ARRAY['cold_smtp'::text, 'oauth_api'::text, 'warmup_only'::text]))),
    CONSTRAINT workers_health_state_check CHECK ((health_state = ANY (ARRAY['healthy'::text, 'watch'::text, 'throttled'::text, 'quarantined'::text, 'blocked'::text])))
);

--
-- Name: worker_capacity_view; Type: MATERIALIZED VIEW; Schema: public; Owner: -
--

CREATE MATERIALIZED VIEW public.worker_capacity_view AS
 WITH aggregated AS (
         SELECT worker_health_samples.worker_id,
            sum(worker_health_samples.sends_attempted) AS sends_attempted_1h,
            sum(worker_health_samples.sends_succeeded) AS sends_succeeded_1h,
            sum(worker_health_samples.bounces_hard) AS bounces_hard_1h,
            sum(worker_health_samples.bounces_soft) AS bounces_soft_1h,
            sum(worker_health_samples.complaints) AS complaints_1h,
            sum(worker_health_samples.auth_errors) AS auth_errors_1h
           FROM public.worker_health_samples
          WHERE (worker_health_samples.observed_at > (now() - '01:00:00'::interval))
          GROUP BY worker_health_samples.worker_id
        )
 SELECT w.id AS worker_id,
    w.worker_type,
    w.free_tier,
    w.egress_kind,
    w.health_state,
    w.load_score,
    (
        CASE w.egress_kind
            WHEN 'cold_smtp'::text THEN 16
            WHEN 'oauth_api'::text THEN 400
            WHEN 'warmup_only'::text THEN 25
            ELSE 16
        END)::numeric AS base_capacity,
    GREATEST(0.0, LEAST(1.0, ((1.0 - LEAST(0.5, (((COALESCE(a.bounces_hard_1h, (0)::bigint))::numeric / (NULLIF(a.sends_attempted_1h, 0))::numeric) * (5)::numeric))) - LEAST(0.5, (((COALESCE(a.complaints_1h, (0)::bigint))::numeric / (NULLIF(a.sends_attempted_1h, 0))::numeric) * (100)::numeric))))) AS health_multiplier,
    LEAST(1.0, (EXTRACT(epoch FROM (now() - w.created_at)) / ((72 * 3600))::numeric)) AS age_multiplier,
    COALESCE(a.sends_attempted_1h, (0)::bigint) AS sends_attempted_1h,
    COALESCE(a.sends_succeeded_1h, (0)::bigint) AS sends_succeeded_1h,
    COALESCE(a.bounces_hard_1h, (0)::bigint) AS bounces_hard_1h,
    COALESCE(a.bounces_soft_1h, (0)::bigint) AS bounces_soft_1h,
    COALESCE(a.complaints_1h, (0)::bigint) AS complaints_1h,
    COALESCE(a.auth_errors_1h, (0)::bigint) AS auth_errors_1h
   FROM (public.workers w
     LEFT JOIN aggregated a ON ((a.worker_id = w.id)))
  WHERE w.active
  WITH NO DATA;

--
-- Name: worker_health_samples_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.worker_health_samples_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

--
-- Name: worker_health_samples_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.worker_health_samples_id_seq OWNED BY public.worker_health_samples.id;

--
-- Name: worker_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.worker_profiles (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    name character varying(120) NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    app_env character varying(20) DEFAULT 'prod'::character varying NOT NULL,
    worker_image text DEFAULT 'ghcr.io/warmbly/worker:latest'::text NOT NULL,
    kafka_bootstrap_servers text DEFAULT ''::text NOT NULL,
    kafka_sasl_username text DEFAULT ''::text NOT NULL,
    kafka_sasl_password_encrypted text DEFAULT ''::text NOT NULL,
    schema_registry_url text DEFAULT ''::text NOT NULL,
    schema_registry_key text DEFAULT ''::text NOT NULL,
    schema_registry_secret_encrypted text DEFAULT ''::text NOT NULL,
    redis_url_encrypted text DEFAULT ''::text NOT NULL,
    aws_credential_id uuid,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    release_channel public.release_channel DEFAULT 'pinned'::public.release_channel NOT NULL,
    auto_update boolean DEFAULT false NOT NULL,
    resolved_image_tag text DEFAULT ''::text NOT NULL,
    last_release_check_at timestamp with time zone
);

--
-- Name: worker_tags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.worker_tags (
    worker_id uuid NOT NULL,
    tag character varying(64) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT valid_tag CHECK (((length((tag)::text) > 0) AND ((tag)::text ~ '^[a-z0-9][a-z0-9._:/-]*$'::text)))
);

--
-- Name: decision_log id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.decision_log ALTER COLUMN id SET DEFAULT nextval('public.decision_log_id_seq'::regclass);

--
-- Name: worker_health_samples id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_health_samples ALTER COLUMN id SET DEFAULT nextval('public.worker_health_samples_id_seq'::regclass);

--
-- Name: admin_audit_logs admin_audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_audit_logs
    ADD CONSTRAINT admin_audit_logs_pkey PRIMARY KEY (id);

--
-- Name: admin_outreach_messages admin_outreach_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_outreach_messages
    ADD CONSTRAINT admin_outreach_messages_pkey PRIMARY KEY (id);

--
-- Name: api_idempotency_keys api_idempotency_keys_organization_id_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_idempotency_keys
    ADD CONSTRAINT api_idempotency_keys_organization_id_key_key UNIQUE (organization_id, key);

--
-- Name: api_idempotency_keys api_idempotency_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_idempotency_keys
    ADD CONSTRAINT api_idempotency_keys_pkey PRIMARY KEY (id);

--
-- Name: api_key_usage_logs api_key_usage_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_key_usage_logs
    ADD CONSTRAINT api_key_usage_logs_pkey PRIMARY KEY (id);

--
-- Name: api_keys api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);

--
-- Name: audit_logs audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (id);

--
-- Name: aws_credentials aws_credentials_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.aws_credentials
    ADD CONSTRAINT aws_credentials_name_key UNIQUE (name);

--
-- Name: aws_credentials aws_credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.aws_credentials
    ADD CONSTRAINT aws_credentials_pkey PRIMARY KEY (id);

--
-- Name: campaign_ab_assignments campaign_ab_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_ab_assignments
    ADD CONSTRAINT campaign_ab_assignments_pkey PRIMARY KEY (campaign_id, contact_id);

--
-- Name: campaign_ab_variants campaign_ab_variants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_ab_variants
    ADD CONSTRAINT campaign_ab_variants_pkey PRIMARY KEY (id);

--
-- Name: campaign_advanced_settings campaign_advanced_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_advanced_settings
    ADD CONSTRAINT campaign_advanced_settings_pkey PRIMARY KEY (campaign_id);

--
-- Name: campaign_contact_progress campaign_contact_progress_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_contact_progress
    ADD CONSTRAINT campaign_contact_progress_pkey PRIMARY KEY (campaign_id, contact_id, sequence_id);

--
-- Name: campaign_email_tags campaign_email_tags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_email_tags
    ADD CONSTRAINT campaign_email_tags_pkey PRIMARY KEY (campaign_id, tag_id);

--
-- Name: campaign_folders campaign_folders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_folders
    ADD CONSTRAINT campaign_folders_pkey PRIMARY KEY (campaign_id, folder_id);

--
-- Name: campaign_leads campaign_leads_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_leads
    ADD CONSTRAINT campaign_leads_pkey PRIMARY KEY (campaign_id, contact_id);

--
-- Name: campaign_logs campaign_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_logs
    ADD CONSTRAINT campaign_logs_pkey PRIMARY KEY (id);

--
-- Name: campaign_tasks campaign_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_tasks
    ADD CONSTRAINT campaign_tasks_pkey PRIMARY KEY (task_id);

--
-- Name: campaign_ab_variants campaign_variant_unique_name; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_ab_variants
    ADD CONSTRAINT campaign_variant_unique_name UNIQUE (campaign_id, name);

--
-- Name: campaigns campaigns_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaigns
    ADD CONSTRAINT campaigns_pkey PRIMARY KEY (id);

--
-- Name: categories categories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.categories
    ADD CONSTRAINT categories_pkey PRIMARY KEY (id);

--
-- Name: cloud_credentials cloud_credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cloud_credentials
    ADD CONSTRAINT cloud_credentials_pkey PRIMARY KEY (id);

--
-- Name: contact_activities contact_activities_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_activities
    ADD CONSTRAINT contact_activities_pkey PRIMARY KEY (id);

--
-- Name: contact_categories contact_categories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_categories
    ADD CONSTRAINT contact_categories_pkey PRIMARY KEY (contact_id, category_id);

--
-- Name: contact_notes contact_notes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_notes
    ADD CONSTRAINT contact_notes_pkey PRIMARY KEY (id);

--
-- Name: contacts contacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contacts
    ADD CONSTRAINT contacts_pkey PRIMARY KEY (id);

--
-- Name: conversation_messages conversation_messages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_messages
    ADD CONSTRAINT conversation_messages_pkey PRIMARY KEY (id);

--
-- Name: conversation_themes conversation_themes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_themes
    ADD CONSTRAINT conversation_themes_pkey PRIMARY KEY (id);

--
-- Name: conversations conversations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversations
    ADD CONSTRAINT conversations_pkey PRIMARY KEY (id);

--
-- Name: crm_tasks crm_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crm_tasks
    ADD CONSTRAINT crm_tasks_pkey PRIMARY KEY (id);

--
-- Name: daily_email_counts daily_email_counts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.daily_email_counts
    ADD CONSTRAINT daily_email_counts_pkey PRIMARY KEY (email_account_id, date);

--
-- Name: deals deals_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deals
    ADD CONSTRAINT deals_pkey PRIMARY KEY (id);

--
-- Name: decision_log decision_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.decision_log
    ADD CONSTRAINT decision_log_pkey PRIMARY KEY (id);

--
-- Name: dedicated_worker_assignments dedicated_worker_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dedicated_worker_assignments
    ADD CONSTRAINT dedicated_worker_assignments_pkey PRIMARY KEY (id);

--
-- Name: deliverability_events deliverability_events_idempotency_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deliverability_events
    ADD CONSTRAINT deliverability_events_idempotency_unique UNIQUE (idempotency_key);

--
-- Name: deliverability_events deliverability_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deliverability_events
    ADD CONSTRAINT deliverability_events_pkey PRIMARY KEY (id);

--
-- Name: discount_code_plans discount_code_plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_code_plans
    ADD CONSTRAINT discount_code_plans_pkey PRIMARY KEY (discount_code_id, plan_id);

--
-- Name: discount_codes discount_codes_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_codes
    ADD CONSTRAINT discount_codes_code_key UNIQUE (code);

--
-- Name: discount_codes discount_codes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_codes
    ADD CONSTRAINT discount_codes_pkey PRIMARY KEY (id);

--
-- Name: discount_redemptions discount_redemptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_redemptions
    ADD CONSTRAINT discount_redemptions_pkey PRIMARY KEY (id);

--
-- Name: discount_redemptions discount_redemptions_stripe_checkout_session_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_redemptions
    ADD CONSTRAINT discount_redemptions_stripe_checkout_session_id_key UNIQUE (stripe_checkout_session_id);

--
-- Name: durations durations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.durations
    ADD CONSTRAINT durations_pkey PRIMARY KEY (id);

--
-- Name: email_account_errors email_account_errors_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_account_errors
    ADD CONSTRAINT email_account_errors_pkey PRIMARY KEY (id);

--
-- Name: email_accounts_oauth email_accounts_oauth_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_accounts_oauth
    ADD CONSTRAINT email_accounts_oauth_pkey PRIMARY KEY (email_account_id);

--
-- Name: email_accounts email_accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_accounts
    ADD CONSTRAINT email_accounts_pkey PRIMARY KEY (id);

--
-- Name: email_accounts_smtp_imap email_accounts_smtp_imap_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_accounts_smtp_imap
    ADD CONSTRAINT email_accounts_smtp_imap_pkey PRIMARY KEY (email_account_id);

--
-- Name: email_tags email_tags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_tags
    ADD CONSTRAINT email_tags_pkey PRIMARY KEY (email_id, tag_id);

--
-- Name: email_tasks email_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_tasks
    ADD CONSTRAINT email_tasks_pkey PRIMARY KEY (task_id);

--
-- Name: enterprise_inquiries enterprise_inquiries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.enterprise_inquiries
    ADD CONSTRAINT enterprise_inquiries_pkey PRIMARY KEY (id);

--
-- Name: folders folders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.folders
    ADD CONSTRAINT folders_pkey PRIMARY KEY (id);

--
-- Name: integration_connections integration_connections_organization_id_provider_label_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_connections
    ADD CONSTRAINT integration_connections_organization_id_provider_label_key UNIQUE (organization_id, provider, label);

--
-- Name: integration_connections integration_connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_connections
    ADD CONSTRAINT integration_connections_pkey PRIMARY KEY (id);

--
-- Name: integration_event_subscriptions integration_event_subscriptio_connection_id_event_type_acti_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_event_subscriptions
    ADD CONSTRAINT integration_event_subscriptio_connection_id_event_type_acti_key UNIQUE (connection_id, event_type, action);

--
-- Name: integration_event_subscriptions integration_event_subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_event_subscriptions
    ADD CONSTRAINT integration_event_subscriptions_pkey PRIMARY KEY (id);

--
-- Name: integration_field_mappings integration_field_mappings_connection_id_direction_warmbly__key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_field_mappings
    ADD CONSTRAINT integration_field_mappings_connection_id_direction_warmbly__key UNIQUE (connection_id, direction, warmbly_field, external_field);

--
-- Name: integration_field_mappings integration_field_mappings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_field_mappings
    ADD CONSTRAINT integration_field_mappings_pkey PRIMARY KEY (id);

--
-- Name: integration_oauth_states integration_oauth_states_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_oauth_states
    ADD CONSTRAINT integration_oauth_states_pkey PRIMARY KEY (id);

--
-- Name: integration_oauth_states integration_oauth_states_state_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_oauth_states
    ADD CONSTRAINT integration_oauth_states_state_key UNIQUE (state);

--
-- Name: integration_sync_runs integration_sync_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_sync_runs
    ADD CONSTRAINT integration_sync_runs_pkey PRIMARY KEY (id);

--
-- Name: languages languages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.languages
    ADD CONSTRAINT languages_pkey PRIMARY KEY (id);

--
-- Name: limit_increase_requests limit_increase_requests_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.limit_increase_requests
    ADD CONSTRAINT limit_increase_requests_pkey PRIMARY KEY (id);

--
-- Name: meeting_bookings meeting_bookings_organization_id_source_external_event_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.meeting_bookings
    ADD CONSTRAINT meeting_bookings_organization_id_source_external_event_id_key UNIQUE (organization_id, source, external_event_id);

--
-- Name: meeting_bookings meeting_bookings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.meeting_bookings
    ADD CONSTRAINT meeting_bookings_pkey PRIMARY KEY (id);

--
-- Name: offer_options offer_options_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offer_options
    ADD CONSTRAINT offer_options_pkey PRIMARY KEY (offer_id, plan_id);

--
-- Name: offers offers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offers
    ADD CONSTRAINT offers_pkey PRIMARY KEY (id);

--
-- Name: organization_invitations organization_invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_invitations
    ADD CONSTRAINT organization_invitations_pkey PRIMARY KEY (id);

--
-- Name: organization_invitations organization_invitations_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_invitations
    ADD CONSTRAINT organization_invitations_token_key UNIQUE (token);

--
-- Name: organization_limit_overrides organization_limit_overrides_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_limit_overrides
    ADD CONSTRAINT organization_limit_overrides_pkey PRIMARY KEY (organization_id);

--
-- Name: organization_members organization_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_members
    ADD CONSTRAINT organization_members_pkey PRIMARY KEY (id);

--
-- Name: organizations organizations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organizations
    ADD CONSTRAINT organizations_pkey PRIMARY KEY (id);

--
-- Name: organizations organizations_slug_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organizations
    ADD CONSTRAINT organizations_slug_key UNIQUE (slug);

--
-- Name: outreach_settings outreach_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outreach_settings
    ADD CONSTRAINT outreach_settings_pkey PRIMARY KEY (organization_id);

--
-- Name: pipeline_stages pipeline_stages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipeline_stages
    ADD CONSTRAINT pipeline_stages_pkey PRIMARY KEY (id);

--
-- Name: pipelines pipelines_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipelines
    ADD CONSTRAINT pipelines_pkey PRIMARY KEY (id);

--
-- Name: plan_rate_limits plan_rate_limits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plan_rate_limits
    ADD CONSTRAINT plan_rate_limits_pkey PRIMARY KEY (plan_id);

--
-- Name: plans plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_pkey PRIMARY KEY (id);

--
-- Name: platform_statistics platform_statistics_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_statistics
    ADD CONSTRAINT platform_statistics_pkey PRIMARY KEY (id);

--
-- Name: platform_statistics platform_statistics_stat_date_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.platform_statistics
    ADD CONSTRAINT platform_statistics_stat_date_key UNIQUE (stat_date);

--
-- Name: preflight_reports preflight_reports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preflight_reports
    ADD CONSTRAINT preflight_reports_pkey PRIMARY KEY (id);

--
-- Name: provisioning_jobs provisioning_jobs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provisioning_jobs
    ADD CONSTRAINT provisioning_jobs_pkey PRIMARY KEY (id);

--
-- Name: provisioning_policy provisioning_policy_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provisioning_policy
    ADD CONSTRAINT provisioning_policy_pkey PRIMARY KEY (provider);

--
-- Name: provisioning_templates provisioning_templates_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provisioning_templates
    ADD CONSTRAINT provisioning_templates_name_key UNIQUE (name);

--
-- Name: provisioning_templates provisioning_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provisioning_templates
    ADD CONSTRAINT provisioning_templates_pkey PRIMARY KEY (id);

--
-- Name: realtime_events realtime_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.realtime_events
    ADD CONSTRAINT realtime_events_pkey PRIMARY KEY (id);

--
-- Name: reply_intents reply_intents_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reply_intents
    ADD CONSTRAINT reply_intents_pkey PRIMARY KEY (id);

--
-- Name: reply_templates reply_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reply_templates
    ADD CONSTRAINT reply_templates_pkey PRIMARY KEY (id);

--
-- Name: roles roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.roles
    ADD CONSTRAINT roles_pkey PRIMARY KEY (id);

--
-- Name: scheduled_deletions scheduled_deletions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scheduled_deletions
    ADD CONSTRAINT scheduled_deletions_pkey PRIMARY KEY (id);

--
-- Name: secret_plans secret_plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret_plans
    ADD CONSTRAINT secret_plans_pkey PRIMARY KEY (user_id, plan_id);

--
-- Name: sequences sequences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sequences
    ADD CONSTRAINT sequences_pkey PRIMARY KEY (id);

--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);

--
-- Name: storage_backends storage_backends_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.storage_backends
    ADD CONSTRAINT storage_backends_pkey PRIMARY KEY (id);

--
-- Name: stripe_webhook_events stripe_webhook_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.stripe_webhook_events
    ADD CONSTRAINT stripe_webhook_events_pkey PRIMARY KEY (id);

--
-- Name: subscriptions subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);

--
-- Name: subscriptions subscriptions_stripe_subscription_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_stripe_subscription_id_key UNIQUE (stripe_subscription_id);

--
-- Name: suppressed_recipients suppressed_recipients_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.suppressed_recipients
    ADD CONSTRAINT suppressed_recipients_pkey PRIMARY KEY (id);

--
-- Name: suppressed_recipients suppressed_recipients_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.suppressed_recipients
    ADD CONSTRAINT suppressed_recipients_unique UNIQUE (organization_id, email);

--
-- Name: tags tags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tags
    ADD CONSTRAINT tags_pkey PRIMARY KEY (id);

--
-- Name: task_dead_letters task_dead_letters_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_dead_letters
    ADD CONSTRAINT task_dead_letters_pkey PRIMARY KEY (id);

--
-- Name: task_execution_keys task_execution_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_execution_keys
    ADD CONSTRAINT task_execution_keys_pkey PRIMARY KEY (task_id, execution_key);

--
-- Name: task_failures task_failures_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_failures
    ADD CONSTRAINT task_failures_pkey PRIMARY KEY (task_id);

--
-- Name: tasks tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_pkey PRIMARY KEY (id);

--
-- Name: tracking_events_processed tracking_events_processed_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tracking_events_processed
    ADD CONSTRAINT tracking_events_processed_pkey PRIMARY KEY (task_id, event_type, url_hash);

--
-- Name: unibox_emails unibox_emails_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.unibox_emails
    ADD CONSTRAINT unibox_emails_pkey PRIMARY KEY (id);

--
-- Name: unibox_mailboxes unibox_mailboxes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.unibox_mailboxes
    ADD CONSTRAINT unibox_mailboxes_pkey PRIMARY KEY (email_id, uid_validity);

--
-- Name: unibox_snoozes unibox_snoozes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.unibox_snoozes
    ADD CONSTRAINT unibox_snoozes_pkey PRIMARY KEY (id);

--
-- Name: unibox_snoozes unibox_snoozes_user_id_thread_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.unibox_snoozes
    ADD CONSTRAINT unibox_snoozes_user_id_thread_id_key UNIQUE (user_id, thread_id);

--
-- Name: dedicated_worker_assignments unique_active_user_assignment; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dedicated_worker_assignments
    ADD CONSTRAINT unique_active_user_assignment UNIQUE (user_id, released_at);

--
-- Name: dedicated_worker_assignments unique_active_worker; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dedicated_worker_assignments
    ADD CONSTRAINT unique_active_worker UNIQUE (worker_id, released_at);

--
-- Name: organization_members unique_org_member; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_members
    ADD CONSTRAINT unique_org_member UNIQUE (organization_id, user_id);

--
-- Name: subscriptions unique_org_subscription; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT unique_org_subscription UNIQUE (organization_id);

--
-- Name: organization_invitations unique_pending_org_invite; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_invitations
    ADD CONSTRAINT unique_pending_org_invite UNIQUE (organization_id, email);

--
-- Name: user_bans user_bans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_bans
    ADD CONSTRAINT user_bans_pkey PRIMARY KEY (id);

--
-- Name: user_encrypted_keys user_encrypted_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_encrypted_keys
    ADD CONSTRAINT user_encrypted_keys_pkey PRIMARY KEY (user_id);

--
-- Name: user_rate_limits user_rate_limits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_rate_limits
    ADD CONSTRAINT user_rate_limits_pkey PRIMARY KEY (user_id);

--
-- Name: user_roles user_roles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT user_roles_pkey PRIMARY KEY (user_id, role_id);

--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);

--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

--
-- Name: warmup_admin_actions warmup_admin_actions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_admin_actions
    ADD CONSTRAINT warmup_admin_actions_pkey PRIMARY KEY (id);

--
-- Name: warmup_appeals warmup_appeals_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_appeals
    ADD CONSTRAINT warmup_appeals_pkey PRIMARY KEY (id);

--
-- Name: warmup_invalid_token_attempts warmup_invalid_token_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_invalid_token_attempts
    ADD CONSTRAINT warmup_invalid_token_attempts_pkey PRIMARY KEY (id);

--
-- Name: warmup_pool_participants warmup_pool_participants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_pool_participants
    ADD CONSTRAINT warmup_pool_participants_pkey PRIMARY KEY (pool_id, email_account_id);

--
-- Name: warmup_pools warmup_pools_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_pools
    ADD CONSTRAINT warmup_pools_pkey PRIMARY KEY (id);

--
-- Name: warmup_routing_rules warmup_routing_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_routing_rules
    ADD CONSTRAINT warmup_routing_rules_pkey PRIMARY KEY (id);

--
-- Name: warmup_spam_reports warmup_spam_reports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_spam_reports
    ADD CONSTRAINT warmup_spam_reports_pkey PRIMARY KEY (id);

--
-- Name: warmup_spam_reports warmup_spam_reports_reporter_account_id_message_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_spam_reports
    ADD CONSTRAINT warmup_spam_reports_reporter_account_id_message_id_key UNIQUE (reporter_account_id, message_id);

--
-- Name: warmup_statistics warmup_statistics_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_statistics
    ADD CONSTRAINT warmup_statistics_pkey PRIMARY KEY (email_account_id, date);

--
-- Name: warmup_tasks warmup_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_tasks
    ADD CONSTRAINT warmup_tasks_pkey PRIMARY KEY (task_id);

--
-- Name: warmup_tokens warmup_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_tokens
    ADD CONSTRAINT warmup_tokens_pkey PRIMARY KEY (token);

--
-- Name: webauthn_credentials webauthn_credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webauthn_credentials
    ADD CONSTRAINT webauthn_credentials_pkey PRIMARY KEY (id);

--
-- Name: webhook_deliveries webhook_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_pkey PRIMARY KEY (id);

--
-- Name: webhook_endpoints webhook_endpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_endpoints
    ADD CONSTRAINT webhook_endpoints_pkey PRIMARY KEY (id);

--
-- Name: worker_health_samples worker_health_samples_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_health_samples
    ADD CONSTRAINT worker_health_samples_pkey PRIMARY KEY (id);

--
-- Name: worker_profiles worker_profiles_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_profiles
    ADD CONSTRAINT worker_profiles_name_key UNIQUE (name);

--
-- Name: worker_profiles worker_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_profiles
    ADD CONSTRAINT worker_profiles_pkey PRIMARY KEY (id);

--
-- Name: worker_tags worker_tags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_tags
    ADD CONSTRAINT worker_tags_pkey PRIMARY KEY (worker_id, tag);

--
-- Name: workers workers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workers
    ADD CONSTRAINT workers_pkey PRIMARY KEY (id);

--
-- Name: idx_admin_audit_action; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_audit_action ON public.admin_audit_logs USING btree (action);

--
-- Name: idx_admin_audit_admin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_audit_admin ON public.admin_audit_logs USING btree (admin_user_id);

--
-- Name: idx_admin_audit_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_audit_created ON public.admin_audit_logs USING btree (created_at DESC);

--
-- Name: idx_admin_audit_target; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_audit_target ON public.admin_audit_logs USING btree (target_type, target_id);

--
-- Name: idx_admin_outreach_sent_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_outreach_sent_by ON public.admin_outreach_messages USING btree (sent_by, created_at DESC);

--
-- Name: idx_admin_outreach_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_outreach_status ON public.admin_outreach_messages USING btree (status, created_at DESC);

--
-- Name: idx_admin_outreach_to_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_outreach_to_org ON public.admin_outreach_messages USING btree (to_org_id) WHERE (to_org_id IS NOT NULL);

--
-- Name: idx_admin_outreach_to_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_admin_outreach_to_user ON public.admin_outreach_messages USING btree (to_user_id) WHERE (to_user_id IS NOT NULL);

--
-- Name: idx_api_idempotency_keys_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_idempotency_keys_expires ON public.api_idempotency_keys USING btree (expires_at);

--
-- Name: idx_api_key_usage_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_key_usage_created ON public.api_key_usage_logs USING btree (created_at);

--
-- Name: idx_api_key_usage_endpoint; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_key_usage_endpoint ON public.api_key_usage_logs USING btree (api_key_id, endpoint);

--
-- Name: idx_api_key_usage_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_key_usage_key ON public.api_key_usage_logs USING btree (api_key_id, created_at DESC);

--
-- Name: idx_api_key_usage_key_time_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_key_usage_key_time_status ON public.api_key_usage_logs USING btree (api_key_id, created_at DESC, response_status);

--
-- Name: idx_api_keys_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_api_keys_hash ON public.api_keys USING btree (key_hash) WHERE ((status)::text = 'active'::text);

--
-- Name: idx_api_keys_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_org ON public.api_keys USING btree (organization_id, status);

--
-- Name: idx_api_keys_prefix; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_prefix ON public.api_keys USING btree (user_id, key_prefix);

--
-- Name: idx_api_keys_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_api_keys_user ON public.api_keys USING btree (user_id, status);

--
-- Name: idx_audit_logs_action; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_action ON public.audit_logs USING btree (organization_id, action, created_at DESC);

--
-- Name: idx_audit_logs_actor; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_actor ON public.audit_logs USING btree (organization_id, actor_id, created_at DESC);

--
-- Name: idx_audit_logs_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_created ON public.audit_logs USING btree (created_at);

--
-- Name: idx_audit_logs_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_entity ON public.audit_logs USING btree (organization_id, entity_type, entity_id, created_at DESC);

--
-- Name: idx_audit_logs_org_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_org_created ON public.audit_logs USING btree (organization_id, created_at DESC, id DESC);

--
-- Name: idx_campaign_ab_assignments_variant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_ab_assignments_variant ON public.campaign_ab_assignments USING btree (variant_id);

--
-- Name: idx_campaign_ab_variants_campaign; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_ab_variants_campaign ON public.campaign_ab_variants USING btree (campaign_id);

--
-- Name: idx_campaign_leads_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_leads_contact ON public.campaign_leads USING btree (contact_id);

--
-- Name: idx_campaign_logs_campaign; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_logs_campaign ON public.campaign_logs USING btree (campaign_id, created_at DESC);

--
-- Name: idx_campaign_logs_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_logs_type ON public.campaign_logs USING btree (campaign_id, event_type, created_at DESC);

--
-- Name: idx_campaign_progress_campaign; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_campaign ON public.campaign_contact_progress USING btree (campaign_id);

--
-- Name: idx_campaign_progress_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_contact ON public.campaign_contact_progress USING btree (contact_id);

--
-- Name: idx_campaign_progress_daily; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_daily ON public.campaign_contact_progress USING btree (sent_at, campaign_id) WHERE (sent_at IS NOT NULL);

--
-- Name: idx_campaign_progress_org_sent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_org_sent ON public.campaign_contact_progress USING btree (campaign_id, sent_at DESC) WHERE (sent_at IS NOT NULL);

--
-- Name: idx_campaign_progress_recent_bounces; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_recent_bounces ON public.campaign_contact_progress USING btree (bounced_at DESC) WHERE (bounced_at IS NOT NULL);

--
-- Name: idx_campaign_progress_recent_clicks; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_recent_clicks ON public.campaign_contact_progress USING btree (clicked_at DESC) WHERE (clicked_at IS NOT NULL);

--
-- Name: idx_campaign_progress_recent_opens; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_recent_opens ON public.campaign_contact_progress USING btree (opened_at DESC) WHERE (opened_at IS NOT NULL);

--
-- Name: idx_campaign_progress_recent_replies; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_recent_replies ON public.campaign_contact_progress USING btree (replied_at DESC) WHERE (replied_at IS NOT NULL);

--
-- Name: idx_campaign_progress_sent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_progress_sent ON public.campaign_contact_progress USING btree (campaign_id, sent_at) WHERE (sent_at IS NOT NULL);

--
-- Name: idx_campaign_tasks_tracking; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaign_tasks_tracking ON public.campaign_tasks USING btree (task_id) WHERE (contact_id IS NOT NULL);

--
-- Name: idx_campaigns_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_campaigns_org ON public.campaigns USING btree (organization_id);

--
-- Name: idx_contact_activities_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_contact_activities_contact ON public.contact_activities USING btree (contact_id, created_at DESC);

--
-- Name: idx_contact_activities_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_contact_activities_org ON public.contact_activities USING btree (organization_id, created_at DESC);

--
-- Name: idx_contact_categories_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_contact_categories_category ON public.contact_categories USING btree (category_id);

--
-- Name: idx_contact_notes_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_contact_notes_contact ON public.contact_notes USING btree (contact_id, created_at DESC);

--
-- Name: idx_contacts_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_contacts_org ON public.contacts USING btree (organization_id);

--
-- Name: idx_contacts_user_email_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_contacts_user_email_unique ON public.contacts USING btree (user_id, lower(email));

--
-- Name: idx_crm_tasks_assigned; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_crm_tasks_assigned ON public.crm_tasks USING btree (assigned_to, status);

--
-- Name: idx_crm_tasks_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_crm_tasks_contact ON public.crm_tasks USING btree (contact_id);

--
-- Name: idx_crm_tasks_deal; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_crm_tasks_deal ON public.crm_tasks USING btree (deal_id);

--
-- Name: idx_crm_tasks_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_crm_tasks_org ON public.crm_tasks USING btree (organization_id);

--
-- Name: idx_daily_counts_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_daily_counts_date ON public.daily_email_counts USING btree (date);

--
-- Name: idx_deals_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deals_contact ON public.deals USING btree (contact_id);

--
-- Name: idx_deals_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deals_org ON public.deals USING btree (organization_id);

--
-- Name: idx_deals_pipeline; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deals_pipeline ON public.deals USING btree (pipeline_id, stage_id);

--
-- Name: idx_decision_log_kind_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_decision_log_kind_time ON public.decision_log USING btree (kind, created_at DESC);

--
-- Name: idx_decision_log_worker_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_decision_log_worker_time ON public.decision_log USING btree (worker_id, created_at DESC);

--
-- Name: idx_dedicated_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dedicated_user ON public.dedicated_worker_assignments USING btree (user_id) WHERE (released_at IS NULL);

--
-- Name: idx_dedicated_worker; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dedicated_worker ON public.dedicated_worker_assignments USING btree (worker_id) WHERE (released_at IS NULL);

--
-- Name: idx_deliverability_events_campaign_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deliverability_events_campaign_created ON public.deliverability_events USING btree (campaign_id, created_at DESC);

--
-- Name: idx_deliverability_events_org_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deliverability_events_org_created ON public.deliverability_events USING btree (organization_id, created_at DESC);

--
-- Name: idx_deliverability_events_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_deliverability_events_type ON public.deliverability_events USING btree (event_type, created_at DESC);

--
-- Name: idx_discount_code_plans_plan; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_discount_code_plans_plan ON public.discount_code_plans USING btree (plan_id);

--
-- Name: idx_discount_codes_code; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_discount_codes_code ON public.discount_codes USING btree (code);

--
-- Name: idx_discount_codes_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_discount_codes_status ON public.discount_codes USING btree (status, created_at DESC);

--
-- Name: idx_discount_redemptions_code; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_discount_redemptions_code ON public.discount_redemptions USING btree (discount_code_id, redeemed_at DESC);

--
-- Name: idx_discount_redemptions_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_discount_redemptions_org ON public.discount_redemptions USING btree (organization_id, redeemed_at DESC);

--
-- Name: idx_email_account_errors_code; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_account_errors_code ON public.email_account_errors USING btree (email_account_id, error_code) WHERE (resolved_at IS NULL);

--
-- Name: idx_email_account_errors_unresolved; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_account_errors_unresolved ON public.email_account_errors USING btree (email_account_id) WHERE (resolved_at IS NULL);

--
-- Name: idx_email_account_errors_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_account_errors_user ON public.email_account_errors USING btree (user_id, created_at DESC);

--
-- Name: idx_email_accounts_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_accounts_org ON public.email_accounts USING btree (organization_id);

--
-- Name: idx_email_accounts_risk_band; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_accounts_risk_band ON public.email_accounts USING btree (risk_band) WHERE (risk_band <> 'clean'::public.email_risk_band);

--
-- Name: idx_email_accounts_worker; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_accounts_worker ON public.email_accounts USING btree (worker_id) WHERE (worker_id IS NOT NULL);

--
-- Name: idx_email_tasks_thread; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_tasks_thread ON public.email_tasks USING btree (thread_id) WHERE (thread_id IS NOT NULL);

--
-- Name: idx_enterprise_inquiries_assigned; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_enterprise_inquiries_assigned ON public.enterprise_inquiries USING btree (assigned_to) WHERE (assigned_to IS NOT NULL);

--
-- Name: idx_enterprise_inquiries_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_enterprise_inquiries_email ON public.enterprise_inquiries USING btree (contact_email);

--
-- Name: idx_enterprise_inquiries_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_enterprise_inquiries_status ON public.enterprise_inquiries USING btree (status);

--
-- Name: idx_integration_connections_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_integration_connections_org ON public.integration_connections USING btree (organization_id, provider);

--
-- Name: idx_integration_event_subs_dispatch; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_integration_event_subs_dispatch ON public.integration_event_subscriptions USING btree (organization_id, event_type) WHERE enabled;

--
-- Name: idx_integration_oauth_states_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_integration_oauth_states_expires ON public.integration_oauth_states USING btree (expires_at);

--
-- Name: idx_integration_sync_runs_conn; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_integration_sync_runs_conn ON public.integration_sync_runs USING btree (connection_id, started_at DESC);

--
-- Name: idx_limit_requests_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_limit_requests_org ON public.limit_increase_requests USING btree (organization_id, submitted_at DESC);

--
-- Name: idx_limit_requests_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_limit_requests_status ON public.limit_increase_requests USING btree (status, submitted_at DESC);

--
-- Name: idx_meeting_bookings_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_meeting_bookings_contact ON public.meeting_bookings USING btree (contact_id);

--
-- Name: idx_meeting_bookings_recent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_meeting_bookings_recent ON public.meeting_bookings USING btree (organization_id, created_at DESC);

--
-- Name: idx_org_invitations_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_org_invitations_email ON public.organization_invitations USING btree (email);

--
-- Name: idx_org_invitations_token; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_org_invitations_token ON public.organization_invitations USING btree (token);

--
-- Name: idx_org_limit_overrides_granted_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_org_limit_overrides_granted_by ON public.organization_limit_overrides USING btree (granted_by);

--
-- Name: idx_org_members_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_org_members_org ON public.organization_members USING btree (organization_id);

--
-- Name: idx_org_members_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_org_members_user ON public.organization_members USING btree (user_id);

--
-- Name: idx_organizations_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_organizations_owner ON public.organizations USING btree (owner_user_id);

--
-- Name: idx_organizations_pending_deletion; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_organizations_pending_deletion ON public.organizations USING btree (deletion_scheduled_for) WHERE (deletion_scheduled_for IS NOT NULL);

--
-- Name: idx_organizations_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_organizations_slug ON public.organizations USING btree (slug);

--
-- Name: idx_pipeline_stages_pipeline; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pipeline_stages_pipeline ON public.pipeline_stages USING btree (pipeline_id, "position");

--
-- Name: idx_pipelines_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pipelines_org ON public.pipelines USING btree (organization_id);

--
-- Name: idx_plans_stripe_price; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plans_stripe_price ON public.plans USING btree (stripe_price_id);

--
-- Name: idx_plans_stripe_price_yearly; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plans_stripe_price_yearly ON public.plans USING btree (stripe_price_id_yearly);

--
-- Name: idx_preflight_reports_campaign_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_preflight_reports_campaign_created ON public.preflight_reports USING btree (campaign_id, created_at DESC);

--
-- Name: idx_provisioning_jobs_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provisioning_jobs_active ON public.provisioning_jobs USING btree (created_at DESC) WHERE (state <> ALL (ARRAY['completed'::text, 'failed'::text]));

--
-- Name: idx_provisioning_jobs_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provisioning_jobs_created ON public.provisioning_jobs USING btree (created_at DESC);

--
-- Name: idx_provisioning_jobs_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_provisioning_jobs_state ON public.provisioning_jobs USING btree (state);

--
-- Name: idx_realtime_events_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_realtime_events_expires ON public.realtime_events USING btree (expires_at) WHERE (delivered = false);

--
-- Name: idx_realtime_events_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_realtime_events_org ON public.realtime_events USING btree (org_id, created_at) WHERE (delivered = false);

--
-- Name: idx_realtime_events_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_realtime_events_type ON public.realtime_events USING btree (event_type, created_at DESC);

--
-- Name: idx_realtime_events_user_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_realtime_events_user_pending ON public.realtime_events USING btree (user_id, created_at) WHERE (delivered = false);

--
-- Name: idx_reply_intents_org_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_reply_intents_org_created ON public.reply_intents USING btree (organization_id, created_at DESC);

--
-- Name: idx_reply_intents_org_intent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_reply_intents_org_intent ON public.reply_intents USING btree (organization_id, intent, created_at DESC);

--
-- Name: idx_reply_templates_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_reply_templates_org ON public.reply_templates USING btree (organization_id);

--
-- Name: idx_scheduled_deletions_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_deletions_due ON public.scheduled_deletions USING btree (execute_after) WHERE ((status)::text = 'pending'::text);

--
-- Name: idx_scheduled_deletions_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_deletions_org ON public.scheduled_deletions USING btree (organization_id);

--
-- Name: idx_scheduled_deletions_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_deletions_user ON public.scheduled_deletions USING btree (requested_by_user_id);

--
-- Name: idx_sequences_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sequences_org ON public.sequences USING btree (organization_id);

--
-- Name: idx_sessions_current_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sessions_current_org ON public.sessions USING btree (current_organization_id);

--
-- Name: idx_stripe_events_processed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_stripe_events_processed ON public.stripe_webhook_events USING btree (processed_at);

--
-- Name: idx_stripe_events_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_stripe_events_type ON public.stripe_webhook_events USING btree (event_type);

--
-- Name: idx_subscriptions_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_org ON public.subscriptions USING btree (organization_id);

--
-- Name: idx_subscriptions_period_end; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_period_end ON public.subscriptions USING btree (current_period_end);

--
-- Name: idx_subscriptions_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_status ON public.subscriptions USING btree (status);

--
-- Name: idx_subscriptions_stripe_customer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_stripe_customer ON public.subscriptions USING btree (stripe_customer_id);

--
-- Name: idx_subscriptions_stripe_subscription; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_stripe_subscription ON public.subscriptions USING btree (stripe_subscription_id);

--
-- Name: idx_suppressed_recipients_org_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_suppressed_recipients_org_email ON public.suppressed_recipients USING btree (organization_id, email);

--
-- Name: idx_suppressed_recipients_org_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_suppressed_recipients_org_updated ON public.suppressed_recipients USING btree (organization_id, updated_at DESC);

--
-- Name: idx_task_dead_letters_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_dead_letters_status ON public.task_dead_letters USING btree (status, updated_at DESC);

--
-- Name: idx_task_dead_letters_task; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_dead_letters_task ON public.task_dead_letters USING btree (task_id);

--
-- Name: idx_task_execution_keys_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_task_execution_keys_status ON public.task_execution_keys USING btree (status, last_seen_at DESC);

--
-- Name: idx_tasks_account_completed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_account_completed ON public.tasks USING btree (email_account_id, completed_at) WHERE (status = 'completed'::public.task_status);

--
-- Name: idx_tasks_account_scheduled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_account_scheduled ON public.tasks USING btree (email_account_id, scheduled_at) WHERE (status = 'pending'::public.task_status);

--
-- Name: idx_tasks_campaign_completed_today; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_campaign_completed_today ON public.tasks USING btree (email_account_id, completed_at) WHERE ((status = 'completed'::public.task_status) AND (task_type = 'campaign'::public.task_type));

--
-- Name: idx_tasks_scheduled_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_scheduled_date ON public.tasks USING btree (scheduled_at) WHERE (status = 'pending'::public.task_status);

--
-- Name: idx_tracking_processed_cleanup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tracking_processed_cleanup ON public.tracking_events_processed USING btree (processed_at);

--
-- Name: idx_trial_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_trial_expiry ON public.subscriptions USING btree (free_trial_ends_at) WHERE (free_trial_ends_at IS NOT NULL);

--
-- Name: idx_unibox_emails_from; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_unibox_emails_from ON public.unibox_emails USING gin (from_addr);

--
-- Name: idx_unibox_emails_search; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_unibox_emails_search ON public.unibox_emails USING gin (search_tsv);

--
-- Name: idx_unibox_emails_thread; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_unibox_emails_thread ON public.unibox_emails USING btree (user_id, email_id, thread_id, internal_date DESC);

--
-- Name: idx_unibox_emails_unseen; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_unibox_emails_unseen ON public.unibox_emails USING btree (user_id) WHERE (seen = false);

--
-- Name: idx_unibox_emails_unseen_account; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_unibox_emails_unseen_account ON public.unibox_emails USING btree (user_id, email_id) WHERE (seen = false);

--
-- Name: idx_unibox_emails_user_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_unibox_emails_user_date ON public.unibox_emails USING btree (user_id, internal_date DESC);

--
-- Name: idx_unibox_emails_user_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_unibox_emails_user_email ON public.unibox_emails USING btree (user_id, email_id, internal_date DESC);

--
-- Name: idx_unique_active_dedicated_user; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_unique_active_dedicated_user ON public.dedicated_worker_assignments USING btree (user_id) WHERE (released_at IS NULL);

--
-- Name: idx_user_bans_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_bans_created ON public.user_bans USING btree (banned_at DESC);

--
-- Name: idx_user_bans_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_bans_user ON public.user_bans USING btree (user_id) WHERE (unbanned_at IS NULL);

--
-- Name: idx_users_admin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_admin ON public.users USING btree (admin_permissions) WHERE (admin_permissions > 0);

--
-- Name: idx_users_banned; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_banned ON public.users USING btree (banned_at) WHERE (banned_at IS NOT NULL);

--
-- Name: idx_users_pending_deletion; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_pending_deletion ON public.users USING btree (deletion_scheduled_for) WHERE (deletion_scheduled_for IS NOT NULL);

--
-- Name: idx_warmup_appeals_account; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_appeals_account ON public.warmup_appeals USING btree (email_account_id);

--
-- Name: idx_warmup_appeals_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_appeals_status ON public.warmup_appeals USING btree (status) WHERE ((status)::text = 'pending'::text);

--
-- Name: idx_warmup_invalid_attempts_account; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_invalid_attempts_account ON public.warmup_invalid_token_attempts USING btree (email_account_id, created_at);

--
-- Name: idx_warmup_participants_account; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_participants_account ON public.warmup_pool_participants USING btree (email_account_id);

--
-- Name: idx_warmup_participants_account_health; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_participants_account_health ON public.warmup_pool_participants USING btree (email_account_id, health_state, blocked_until);

--
-- Name: idx_warmup_participants_health_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_participants_health_state ON public.warmup_pool_participants USING btree (health_state, blocked_until);

--
-- Name: idx_warmup_participants_pool; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_participants_pool ON public.warmup_pool_participants USING btree (pool_id) WHERE (blocked_at IS NULL);

--
-- Name: idx_warmup_participants_pool_role_health; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_participants_pool_role_health ON public.warmup_pool_participants USING btree (pool_id, participant_role, health_state, blocked_until);

--
-- Name: idx_warmup_routing_rules_org_priority; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_routing_rules_org_priority ON public.warmup_routing_rules USING btree (organization_id, priority) WHERE enabled;

--
-- Name: idx_warmup_spam_reports_reported; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_spam_reports_reported ON public.warmup_spam_reports USING btree (reported_account_id);

--
-- Name: idx_warmup_spam_reports_reporter; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_spam_reports_reporter ON public.warmup_spam_reports USING btree (reporter_account_id);

--
-- Name: idx_warmup_spam_reports_type_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_spam_reports_type_created ON public.warmup_spam_reports USING btree (report_type, created_at);

--
-- Name: idx_warmup_stats_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_stats_date ON public.warmup_statistics USING btree (date);

--
-- Name: idx_warmup_tokens_recipient; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_tokens_recipient ON public.warmup_tokens USING btree (recipient_account_id);

--
-- Name: idx_warmup_tokens_sender_recipient; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_tokens_sender_recipient ON public.warmup_tokens USING btree (sender_account_id, recipient_account_id);

--
-- Name: idx_warmup_tokens_task; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_tokens_task ON public.warmup_tokens USING btree (task_id);

--
-- Name: idx_warmup_tokens_unconsumed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_warmup_tokens_unconsumed ON public.warmup_tokens USING btree (token) WHERE (consumed_at IS NULL);

--
-- Name: idx_webhook_deliveries_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_deliveries_due ON public.webhook_deliveries USING btree (next_attempt_at) WHERE (status = 'pending'::text);

--
-- Name: idx_webhook_deliveries_endpoint; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_deliveries_endpoint ON public.webhook_deliveries USING btree (endpoint_id, created_at DESC);

--
-- Name: idx_webhook_deliveries_endpoint_event; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_webhook_deliveries_endpoint_event ON public.webhook_deliveries USING btree (endpoint_id, event_id);

--
-- Name: idx_webhook_endpoints_event_types; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_endpoints_event_types ON public.webhook_endpoints USING gin (event_types) WHERE enabled;

--
-- Name: idx_webhook_endpoints_org; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_endpoints_org ON public.webhook_endpoints USING btree (organization_id) WHERE enabled;

--
-- Name: idx_worker_health_samples_worker_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_worker_health_samples_worker_time ON public.worker_health_samples USING btree (worker_id, observed_at DESC);

--
-- Name: idx_worker_profiles_aws; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_worker_profiles_aws ON public.worker_profiles USING btree (aws_credential_id);

--
-- Name: idx_worker_profiles_channel; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_worker_profiles_channel ON public.worker_profiles USING btree (release_channel) WHERE (release_channel <> 'pinned'::public.release_channel);

--
-- Name: idx_worker_tags_tag; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_worker_tags_tag ON public.worker_tags USING btree (tag);

--
-- Name: idx_workers_enrollment_token; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workers_enrollment_token ON public.workers USING btree (enrollment_token_hash) WHERE (enrollment_token_hash IS NOT NULL);

--
-- Name: idx_workers_free_tier; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workers_free_tier ON public.workers USING btree (account_count) WHERE ((worker_type = 'shared'::public.worker_type) AND (active = true) AND (free_tier = true));

--
-- Name: idx_workers_health_capacity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workers_health_capacity ON public.workers USING btree (worker_type, free_tier, health_state, load_score) WHERE (active = true);

--
-- Name: idx_workers_install_state; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workers_install_state ON public.workers USING btree (install_state);

--
-- Name: idx_workers_premium_tier; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workers_premium_tier ON public.workers USING btree (account_count) WHERE ((worker_type = 'shared'::public.worker_type) AND (active = true) AND (free_tier = false));

--
-- Name: idx_workers_profile; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workers_profile ON public.workers USING btree (profile_id);

--
-- Name: idx_workers_risk_pool; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workers_risk_pool ON public.workers USING btree (risk_pool) WHERE (worker_type = 'shared'::public.worker_type);

--
-- Name: idx_workers_shared_load; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workers_shared_load ON public.workers USING btree (account_count) WHERE ((worker_type = 'shared'::public.worker_type) AND (active = true));

--
-- Name: provisioning_templates_auto_per_tier; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX provisioning_templates_auto_per_tier ON public.provisioning_templates USING btree (tier) WHERE is_auto_template;

--
-- Name: storage_backends_active_per_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX storage_backends_active_per_kind ON public.storage_backends USING btree (kind) WHERE is_active;

--
-- Name: unibox_snoozes_user_until; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX unibox_snoozes_user_until ON public.unibox_snoozes USING btree (user_id, snoozed_until);

--
-- Name: uq_limit_requests_one_pending_per_field; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_limit_requests_one_pending_per_field ON public.limit_increase_requests USING btree (organization_id, field) WHERE (status = 'pending'::public.limit_request_status);

--
-- Name: uq_scheduled_deletions_pending_resource; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_scheduled_deletions_pending_resource ON public.scheduled_deletions USING btree (resource_type, resource_id) WHERE ((status)::text = 'pending'::text);

--
-- Name: webauthn_credentials_credential_id_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX webauthn_credentials_credential_id_key ON public.webauthn_credentials USING btree (credential_id);

--
-- Name: webauthn_credentials_user_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX webauthn_credentials_user_id_idx ON public.webauthn_credentials USING btree (user_id, created_at DESC);

--
-- Name: worker_capacity_view_pk; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX worker_capacity_view_pk ON public.worker_capacity_view USING btree (worker_id);

--
-- Name: admin_audit_logs admin_audit_logs_admin_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_audit_logs
    ADD CONSTRAINT admin_audit_logs_admin_user_id_fkey FOREIGN KEY (admin_user_id) REFERENCES public.users(id);

--
-- Name: admin_outreach_messages admin_outreach_messages_sent_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_outreach_messages
    ADD CONSTRAINT admin_outreach_messages_sent_by_fkey FOREIGN KEY (sent_by) REFERENCES public.users(id);

--
-- Name: admin_outreach_messages admin_outreach_messages_to_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_outreach_messages
    ADD CONSTRAINT admin_outreach_messages_to_org_id_fkey FOREIGN KEY (to_org_id) REFERENCES public.organizations(id) ON DELETE SET NULL;

--
-- Name: admin_outreach_messages admin_outreach_messages_to_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.admin_outreach_messages
    ADD CONSTRAINT admin_outreach_messages_to_user_id_fkey FOREIGN KEY (to_user_id) REFERENCES public.users(id) ON DELETE SET NULL;

--
-- Name: api_idempotency_keys api_idempotency_keys_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_idempotency_keys
    ADD CONSTRAINT api_idempotency_keys_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: api_key_usage_logs api_key_usage_logs_api_key_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_key_usage_logs
    ADD CONSTRAINT api_key_usage_logs_api_key_id_fkey FOREIGN KEY (api_key_id) REFERENCES public.api_keys(id) ON DELETE CASCADE;

--
-- Name: api_keys api_keys_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: api_keys api_keys_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: audit_logs audit_logs_actor_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES public.users(id) ON DELETE SET NULL;

--
-- Name: audit_logs audit_logs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: campaign_ab_assignments campaign_ab_assignments_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_ab_assignments
    ADD CONSTRAINT campaign_ab_assignments_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_ab_assignments campaign_ab_assignments_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_ab_assignments
    ADD CONSTRAINT campaign_ab_assignments_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE CASCADE;

--
-- Name: campaign_ab_assignments campaign_ab_assignments_variant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_ab_assignments
    ADD CONSTRAINT campaign_ab_assignments_variant_id_fkey FOREIGN KEY (variant_id) REFERENCES public.campaign_ab_variants(id) ON DELETE CASCADE;

--
-- Name: campaign_ab_variants campaign_ab_variants_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_ab_variants
    ADD CONSTRAINT campaign_ab_variants_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_advanced_settings campaign_advanced_settings_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_advanced_settings
    ADD CONSTRAINT campaign_advanced_settings_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_contact_progress campaign_contact_progress_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_contact_progress
    ADD CONSTRAINT campaign_contact_progress_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_contact_progress campaign_contact_progress_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_contact_progress
    ADD CONSTRAINT campaign_contact_progress_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE CASCADE;

--
-- Name: campaign_contact_progress campaign_contact_progress_sequence_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_contact_progress
    ADD CONSTRAINT campaign_contact_progress_sequence_id_fkey FOREIGN KEY (sequence_id) REFERENCES public.sequences(id) ON DELETE CASCADE;

--
-- Name: campaign_email_tags campaign_email_tags_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_email_tags
    ADD CONSTRAINT campaign_email_tags_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_email_tags campaign_email_tags_tag_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_email_tags
    ADD CONSTRAINT campaign_email_tags_tag_id_fkey FOREIGN KEY (tag_id) REFERENCES public.tags(id) ON DELETE CASCADE;

--
-- Name: campaign_folders campaign_folders_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_folders
    ADD CONSTRAINT campaign_folders_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_folders campaign_folders_folder_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_folders
    ADD CONSTRAINT campaign_folders_folder_id_fkey FOREIGN KEY (folder_id) REFERENCES public.folders(id) ON DELETE CASCADE;

--
-- Name: campaign_leads campaign_leads_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_leads
    ADD CONSTRAINT campaign_leads_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_leads campaign_leads_campaign_id_fkey1; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_leads
    ADD CONSTRAINT campaign_leads_campaign_id_fkey1 FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_leads campaign_leads_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_leads
    ADD CONSTRAINT campaign_leads_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE CASCADE;

--
-- Name: campaign_logs campaign_logs_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_logs
    ADD CONSTRAINT campaign_logs_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: campaign_tasks campaign_tasks_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_tasks
    ADD CONSTRAINT campaign_tasks_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE SET NULL;

--
-- Name: campaign_tasks campaign_tasks_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_tasks
    ADD CONSTRAINT campaign_tasks_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE SET NULL;

--
-- Name: campaign_tasks campaign_tasks_sequence_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_tasks
    ADD CONSTRAINT campaign_tasks_sequence_id_fkey FOREIGN KEY (sequence_id) REFERENCES public.sequences(id) ON DELETE SET NULL;

--
-- Name: campaign_tasks campaign_tasks_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaign_tasks
    ADD CONSTRAINT campaign_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: campaigns campaigns_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaigns
    ADD CONSTRAINT campaigns_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);

--
-- Name: campaigns campaigns_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.campaigns
    ADD CONSTRAINT campaigns_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: categories categories_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.categories
    ADD CONSTRAINT categories_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: contact_activities contact_activities_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_activities
    ADD CONSTRAINT contact_activities_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE CASCADE;

--
-- Name: contact_activities contact_activities_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_activities
    ADD CONSTRAINT contact_activities_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: contact_activities contact_activities_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_activities
    ADD CONSTRAINT contact_activities_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);

--
-- Name: contact_categories contact_categories_category_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_categories
    ADD CONSTRAINT contact_categories_category_id_fkey FOREIGN KEY (category_id) REFERENCES public.categories(id) ON DELETE CASCADE;

--
-- Name: contact_categories contact_categories_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_categories
    ADD CONSTRAINT contact_categories_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE CASCADE;

--
-- Name: contact_notes contact_notes_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_notes
    ADD CONSTRAINT contact_notes_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE CASCADE;

--
-- Name: contact_notes contact_notes_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_notes
    ADD CONSTRAINT contact_notes_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: contact_notes contact_notes_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contact_notes
    ADD CONSTRAINT contact_notes_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);

--
-- Name: contacts contacts_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contacts
    ADD CONSTRAINT contacts_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);

--
-- Name: contacts contacts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contacts
    ADD CONSTRAINT contacts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: conversation_messages conversation_messages_conversation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_messages
    ADD CONSTRAINT conversation_messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES public.conversations(id) ON DELETE CASCADE;

--
-- Name: conversation_messages conversation_messages_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversation_messages
    ADD CONSTRAINT conversation_messages_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.conversation_messages(id) ON DELETE CASCADE;

--
-- Name: conversations conversations_language_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversations
    ADD CONSTRAINT conversations_language_fkey FOREIGN KEY (language) REFERENCES public.languages(id) ON DELETE SET NULL;

--
-- Name: conversations conversations_theme_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.conversations
    ADD CONSTRAINT conversations_theme_fkey FOREIGN KEY (theme) REFERENCES public.conversation_themes(id) ON DELETE SET NULL;

--
-- Name: crm_tasks crm_tasks_assigned_to_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crm_tasks
    ADD CONSTRAINT crm_tasks_assigned_to_fkey FOREIGN KEY (assigned_to) REFERENCES public.users(id);

--
-- Name: crm_tasks crm_tasks_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crm_tasks
    ADD CONSTRAINT crm_tasks_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE SET NULL;

--
-- Name: crm_tasks crm_tasks_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crm_tasks
    ADD CONSTRAINT crm_tasks_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);

--
-- Name: crm_tasks crm_tasks_deal_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crm_tasks
    ADD CONSTRAINT crm_tasks_deal_id_fkey FOREIGN KEY (deal_id) REFERENCES public.deals(id) ON DELETE SET NULL;

--
-- Name: crm_tasks crm_tasks_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.crm_tasks
    ADD CONSTRAINT crm_tasks_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: daily_email_counts daily_email_counts_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.daily_email_counts
    ADD CONSTRAINT daily_email_counts_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: deals deals_assigned_to_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deals
    ADD CONSTRAINT deals_assigned_to_fkey FOREIGN KEY (assigned_to) REFERENCES public.users(id);

--
-- Name: deals deals_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deals
    ADD CONSTRAINT deals_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE SET NULL;

--
-- Name: deals deals_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deals
    ADD CONSTRAINT deals_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: deals deals_pipeline_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deals
    ADD CONSTRAINT deals_pipeline_id_fkey FOREIGN KEY (pipeline_id) REFERENCES public.pipelines(id) ON DELETE CASCADE;

--
-- Name: deals deals_stage_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deals
    ADD CONSTRAINT deals_stage_id_fkey FOREIGN KEY (stage_id) REFERENCES public.pipeline_stages(id);

--
-- Name: dedicated_worker_assignments dedicated_worker_assignments_subscription_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dedicated_worker_assignments
    ADD CONSTRAINT dedicated_worker_assignments_subscription_id_fkey FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id) ON DELETE CASCADE;

--
-- Name: dedicated_worker_assignments dedicated_worker_assignments_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dedicated_worker_assignments
    ADD CONSTRAINT dedicated_worker_assignments_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: dedicated_worker_assignments dedicated_worker_assignments_worker_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dedicated_worker_assignments
    ADD CONSTRAINT dedicated_worker_assignments_worker_id_fkey FOREIGN KEY (worker_id) REFERENCES public.workers(id) ON DELETE CASCADE;

--
-- Name: deliverability_events deliverability_events_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deliverability_events
    ADD CONSTRAINT deliverability_events_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE SET NULL;

--
-- Name: deliverability_events deliverability_events_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deliverability_events
    ADD CONSTRAINT deliverability_events_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE SET NULL;

--
-- Name: deliverability_events deliverability_events_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deliverability_events
    ADD CONSTRAINT deliverability_events_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: deliverability_events deliverability_events_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.deliverability_events
    ADD CONSTRAINT deliverability_events_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE SET NULL;

--
-- Name: discount_code_plans discount_code_plans_discount_code_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_code_plans
    ADD CONSTRAINT discount_code_plans_discount_code_id_fkey FOREIGN KEY (discount_code_id) REFERENCES public.discount_codes(id) ON DELETE CASCADE;

--
-- Name: discount_code_plans discount_code_plans_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_code_plans
    ADD CONSTRAINT discount_code_plans_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id) ON DELETE CASCADE;

--
-- Name: discount_codes discount_codes_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_codes
    ADD CONSTRAINT discount_codes_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;

--
-- Name: discount_redemptions discount_redemptions_discount_code_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_redemptions
    ADD CONSTRAINT discount_redemptions_discount_code_id_fkey FOREIGN KEY (discount_code_id) REFERENCES public.discount_codes(id) ON DELETE CASCADE;

--
-- Name: discount_redemptions discount_redemptions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_redemptions
    ADD CONSTRAINT discount_redemptions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: discount_redemptions discount_redemptions_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_redemptions
    ADD CONSTRAINT discount_redemptions_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id) ON DELETE SET NULL;

--
-- Name: discount_redemptions discount_redemptions_redeemed_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_redemptions
    ADD CONSTRAINT discount_redemptions_redeemed_by_fkey FOREIGN KEY (redeemed_by) REFERENCES public.users(id) ON DELETE SET NULL;

--
-- Name: discount_redemptions discount_redemptions_subscription_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.discount_redemptions
    ADD CONSTRAINT discount_redemptions_subscription_id_fkey FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id) ON DELETE SET NULL;

--
-- Name: email_account_errors email_account_errors_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_account_errors
    ADD CONSTRAINT email_account_errors_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: email_account_errors email_account_errors_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_account_errors
    ADD CONSTRAINT email_account_errors_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: email_accounts_oauth email_accounts_oauth_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_accounts_oauth
    ADD CONSTRAINT email_accounts_oauth_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: email_accounts email_accounts_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_accounts
    ADD CONSTRAINT email_accounts_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);

--
-- Name: email_accounts_smtp_imap email_accounts_smtp_imap_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_accounts_smtp_imap
    ADD CONSTRAINT email_accounts_smtp_imap_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: email_accounts email_accounts_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_accounts
    ADD CONSTRAINT email_accounts_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: email_tags email_tags_email_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_tags
    ADD CONSTRAINT email_tags_email_id_fkey FOREIGN KEY (email_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: email_tags email_tags_tag_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_tags
    ADD CONSTRAINT email_tags_tag_id_fkey FOREIGN KEY (tag_id) REFERENCES public.tags(id) ON DELETE CASCADE;

--
-- Name: email_tasks email_tasks_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_tasks
    ADD CONSTRAINT email_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: enterprise_inquiries enterprise_inquiries_assigned_to_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.enterprise_inquiries
    ADD CONSTRAINT enterprise_inquiries_assigned_to_fkey FOREIGN KEY (assigned_to) REFERENCES public.users(id);

--
-- Name: enterprise_inquiries enterprise_inquiries_processed_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.enterprise_inquiries
    ADD CONSTRAINT enterprise_inquiries_processed_by_fkey FOREIGN KEY (processed_by) REFERENCES public.users(id);

--
-- Name: enterprise_inquiries enterprise_inquiries_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.enterprise_inquiries
    ADD CONSTRAINT enterprise_inquiries_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);

--
-- Name: email_accounts fk_email_accounts_worker; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_accounts
    ADD CONSTRAINT fk_email_accounts_worker FOREIGN KEY (worker_id) REFERENCES public.workers(id) ON DELETE SET NULL;

--
-- Name: folders folders_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.folders
    ADD CONSTRAINT folders_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: integration_connections integration_connections_connected_by_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_connections
    ADD CONSTRAINT integration_connections_connected_by_user_id_fkey FOREIGN KEY (connected_by_user_id) REFERENCES public.users(id) ON DELETE SET NULL;

--
-- Name: integration_connections integration_connections_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_connections
    ADD CONSTRAINT integration_connections_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: integration_event_subscriptions integration_event_subscriptions_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_event_subscriptions
    ADD CONSTRAINT integration_event_subscriptions_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.integration_connections(id) ON DELETE CASCADE;

--
-- Name: integration_event_subscriptions integration_event_subscriptions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_event_subscriptions
    ADD CONSTRAINT integration_event_subscriptions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: integration_field_mappings integration_field_mappings_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_field_mappings
    ADD CONSTRAINT integration_field_mappings_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.integration_connections(id) ON DELETE CASCADE;

--
-- Name: integration_field_mappings integration_field_mappings_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_field_mappings
    ADD CONSTRAINT integration_field_mappings_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: integration_oauth_states integration_oauth_states_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_oauth_states
    ADD CONSTRAINT integration_oauth_states_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: integration_oauth_states integration_oauth_states_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_oauth_states
    ADD CONSTRAINT integration_oauth_states_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: integration_sync_runs integration_sync_runs_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_sync_runs
    ADD CONSTRAINT integration_sync_runs_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.integration_connections(id) ON DELETE CASCADE;

--
-- Name: integration_sync_runs integration_sync_runs_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.integration_sync_runs
    ADD CONSTRAINT integration_sync_runs_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: limit_increase_requests limit_increase_requests_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.limit_increase_requests
    ADD CONSTRAINT limit_increase_requests_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: limit_increase_requests limit_increase_requests_reviewed_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.limit_increase_requests
    ADD CONSTRAINT limit_increase_requests_reviewed_by_fkey FOREIGN KEY (reviewed_by) REFERENCES public.users(id);

--
-- Name: limit_increase_requests limit_increase_requests_submitted_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.limit_increase_requests
    ADD CONSTRAINT limit_increase_requests_submitted_by_fkey FOREIGN KEY (submitted_by) REFERENCES public.users(id);

--
-- Name: meeting_bookings meeting_bookings_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.meeting_bookings
    ADD CONSTRAINT meeting_bookings_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE SET NULL;

--
-- Name: meeting_bookings meeting_bookings_contact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.meeting_bookings
    ADD CONSTRAINT meeting_bookings_contact_id_fkey FOREIGN KEY (contact_id) REFERENCES public.contacts(id) ON DELETE SET NULL;

--
-- Name: meeting_bookings meeting_bookings_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.meeting_bookings
    ADD CONSTRAINT meeting_bookings_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: offer_options offer_options_offer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offer_options
    ADD CONSTRAINT offer_options_offer_id_fkey FOREIGN KEY (offer_id) REFERENCES public.offers(id) ON DELETE CASCADE;

--
-- Name: offer_options offer_options_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.offer_options
    ADD CONSTRAINT offer_options_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id) ON DELETE CASCADE;

--
-- Name: organization_invitations organization_invitations_invited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_invitations
    ADD CONSTRAINT organization_invitations_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id);

--
-- Name: organization_invitations organization_invitations_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_invitations
    ADD CONSTRAINT organization_invitations_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: organization_limit_overrides organization_limit_overrides_granted_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_limit_overrides
    ADD CONSTRAINT organization_limit_overrides_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES public.users(id) ON DELETE SET NULL;

--
-- Name: organization_limit_overrides organization_limit_overrides_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_limit_overrides
    ADD CONSTRAINT organization_limit_overrides_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: organization_members organization_members_invited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_members
    ADD CONSTRAINT organization_members_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id);

--
-- Name: organization_members organization_members_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_members
    ADD CONSTRAINT organization_members_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: organization_members organization_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organization_members
    ADD CONSTRAINT organization_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: organizations organizations_owner_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.organizations
    ADD CONSTRAINT organizations_owner_user_id_fkey FOREIGN KEY (owner_user_id) REFERENCES public.users(id);

--
-- Name: outreach_settings outreach_settings_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outreach_settings
    ADD CONSTRAINT outreach_settings_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: outreach_settings outreach_settings_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.outreach_settings
    ADD CONSTRAINT outreach_settings_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id);

--
-- Name: pipeline_stages pipeline_stages_pipeline_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipeline_stages
    ADD CONSTRAINT pipeline_stages_pipeline_id_fkey FOREIGN KEY (pipeline_id) REFERENCES public.pipelines(id) ON DELETE CASCADE;

--
-- Name: pipelines pipelines_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pipelines
    ADD CONSTRAINT pipelines_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: plan_rate_limits plan_rate_limits_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plan_rate_limits
    ADD CONSTRAINT plan_rate_limits_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id) ON DELETE CASCADE;

--
-- Name: plans plans_duration_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_duration_id_fkey FOREIGN KEY (duration_id) REFERENCES public.durations(id) ON DELETE CASCADE;

--
-- Name: preflight_reports preflight_reports_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preflight_reports
    ADD CONSTRAINT preflight_reports_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: preflight_reports preflight_reports_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preflight_reports
    ADD CONSTRAINT preflight_reports_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: provisioning_jobs provisioning_jobs_credential_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provisioning_jobs
    ADD CONSTRAINT provisioning_jobs_credential_id_fkey FOREIGN KEY (credential_id) REFERENCES public.cloud_credentials(id) ON DELETE SET NULL;

--
-- Name: provisioning_jobs provisioning_jobs_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provisioning_jobs
    ADD CONSTRAINT provisioning_jobs_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.provisioning_templates(id) ON DELETE SET NULL;

--
-- Name: provisioning_templates provisioning_templates_worker_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.provisioning_templates
    ADD CONSTRAINT provisioning_templates_worker_profile_id_fkey FOREIGN KEY (worker_profile_id) REFERENCES public.worker_profiles(id) ON DELETE SET NULL;

--
-- Name: realtime_events realtime_events_org_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.realtime_events
    ADD CONSTRAINT realtime_events_org_id_fkey FOREIGN KEY (org_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: realtime_events realtime_events_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.realtime_events
    ADD CONSTRAINT realtime_events_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: reply_intents reply_intents_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reply_intents
    ADD CONSTRAINT reply_intents_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE SET NULL;

--
-- Name: reply_intents reply_intents_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reply_intents
    ADD CONSTRAINT reply_intents_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: reply_intents reply_intents_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reply_intents
    ADD CONSTRAINT reply_intents_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE SET NULL;

--
-- Name: reply_templates reply_templates_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reply_templates
    ADD CONSTRAINT reply_templates_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: reply_templates reply_templates_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.reply_templates
    ADD CONSTRAINT reply_templates_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: scheduled_deletions scheduled_deletions_cancelled_by_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scheduled_deletions
    ADD CONSTRAINT scheduled_deletions_cancelled_by_user_id_fkey FOREIGN KEY (cancelled_by_user_id) REFERENCES public.users(id);

--
-- Name: scheduled_deletions scheduled_deletions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scheduled_deletions
    ADD CONSTRAINT scheduled_deletions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: scheduled_deletions scheduled_deletions_requested_by_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scheduled_deletions
    ADD CONSTRAINT scheduled_deletions_requested_by_user_id_fkey FOREIGN KEY (requested_by_user_id) REFERENCES public.users(id);

--
-- Name: secret_plans secret_plans_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret_plans
    ADD CONSTRAINT secret_plans_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id) ON DELETE CASCADE;

--
-- Name: secret_plans secret_plans_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secret_plans
    ADD CONSTRAINT secret_plans_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: sequences sequences_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sequences
    ADD CONSTRAINT sequences_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE CASCADE;

--
-- Name: sequences sequences_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sequences
    ADD CONSTRAINT sequences_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id);

--
-- Name: sessions sessions_current_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_current_organization_id_fkey FOREIGN KEY (current_organization_id) REFERENCES public.organizations(id);

--
-- Name: sessions sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: subscriptions subscriptions_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: subscriptions subscriptions_plan_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_plan_id_fkey FOREIGN KEY (plan_id) REFERENCES public.plans(id) ON DELETE RESTRICT;

--
-- Name: subscriptions subscriptions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: suppressed_recipients suppressed_recipients_campaign_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.suppressed_recipients
    ADD CONSTRAINT suppressed_recipients_campaign_id_fkey FOREIGN KEY (campaign_id) REFERENCES public.campaigns(id) ON DELETE SET NULL;

--
-- Name: suppressed_recipients suppressed_recipients_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.suppressed_recipients
    ADD CONSTRAINT suppressed_recipients_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: tags tags_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tags
    ADD CONSTRAINT tags_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: task_dead_letters task_dead_letters_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_dead_letters
    ADD CONSTRAINT task_dead_letters_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: task_execution_keys task_execution_keys_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_execution_keys
    ADD CONSTRAINT task_execution_keys_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: task_failures task_failures_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.task_failures
    ADD CONSTRAINT task_failures_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: tasks tasks_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id);

--
-- Name: unibox_emails unibox_emails_email_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.unibox_emails
    ADD CONSTRAINT unibox_emails_email_id_fkey FOREIGN KEY (email_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: unibox_emails unibox_emails_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.unibox_emails
    ADD CONSTRAINT unibox_emails_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: unibox_mailboxes unibox_mailboxes_email_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.unibox_mailboxes
    ADD CONSTRAINT unibox_mailboxes_email_id_fkey FOREIGN KEY (email_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: unibox_snoozes unibox_snoozes_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.unibox_snoozes
    ADD CONSTRAINT unibox_snoozes_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: user_bans user_bans_banned_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_bans
    ADD CONSTRAINT user_bans_banned_by_fkey FOREIGN KEY (banned_by) REFERENCES public.users(id);

--
-- Name: user_bans user_bans_unbanned_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_bans
    ADD CONSTRAINT user_bans_unbanned_by_fkey FOREIGN KEY (unbanned_by) REFERENCES public.users(id);

--
-- Name: user_bans user_bans_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_bans
    ADD CONSTRAINT user_bans_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: user_encrypted_keys user_encrypted_keys_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_encrypted_keys
    ADD CONSTRAINT user_encrypted_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: user_rate_limits user_rate_limits_updated_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_rate_limits
    ADD CONSTRAINT user_rate_limits_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES public.users(id);

--
-- Name: user_rate_limits user_rate_limits_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_rate_limits
    ADD CONSTRAINT user_rate_limits_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: user_roles user_roles_role_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT user_roles_role_id_fkey FOREIGN KEY (role_id) REFERENCES public.roles(id) ON DELETE CASCADE;

--
-- Name: user_roles user_roles_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_roles
    ADD CONSTRAINT user_roles_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: users users_admin_granted_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_admin_granted_by_fkey FOREIGN KEY (admin_granted_by) REFERENCES public.users(id);

--
-- Name: warmup_admin_actions warmup_admin_actions_admin_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_admin_actions
    ADD CONSTRAINT warmup_admin_actions_admin_user_id_fkey FOREIGN KEY (admin_user_id) REFERENCES public.users(id);

--
-- Name: warmup_admin_actions warmup_admin_actions_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_admin_actions
    ADD CONSTRAINT warmup_admin_actions_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id);

--
-- Name: warmup_appeals warmup_appeals_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_appeals
    ADD CONSTRAINT warmup_appeals_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: warmup_appeals warmup_appeals_reviewed_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_appeals
    ADD CONSTRAINT warmup_appeals_reviewed_by_fkey FOREIGN KEY (reviewed_by) REFERENCES public.users(id);

--
-- Name: warmup_appeals warmup_appeals_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_appeals
    ADD CONSTRAINT warmup_appeals_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id);

--
-- Name: warmup_invalid_token_attempts warmup_invalid_token_attempts_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_invalid_token_attempts
    ADD CONSTRAINT warmup_invalid_token_attempts_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: warmup_pool_participants warmup_pool_participants_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_pool_participants
    ADD CONSTRAINT warmup_pool_participants_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: warmup_pool_participants warmup_pool_participants_pool_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_pool_participants
    ADD CONSTRAINT warmup_pool_participants_pool_id_fkey FOREIGN KEY (pool_id) REFERENCES public.warmup_pools(id) ON DELETE CASCADE;

--
-- Name: warmup_routing_rules warmup_routing_rules_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_routing_rules
    ADD CONSTRAINT warmup_routing_rules_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: warmup_spam_reports warmup_spam_reports_reported_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_spam_reports
    ADD CONSTRAINT warmup_spam_reports_reported_account_id_fkey FOREIGN KEY (reported_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: warmup_spam_reports warmup_spam_reports_reporter_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_spam_reports
    ADD CONSTRAINT warmup_spam_reports_reporter_account_id_fkey FOREIGN KEY (reporter_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: warmup_statistics warmup_statistics_email_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_statistics
    ADD CONSTRAINT warmup_statistics_email_account_id_fkey FOREIGN KEY (email_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: warmup_tasks warmup_tasks_target_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_tasks
    ADD CONSTRAINT warmup_tasks_target_account_id_fkey FOREIGN KEY (target_account_id) REFERENCES public.email_accounts(id) ON DELETE SET NULL;

--
-- Name: warmup_tasks warmup_tasks_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_tasks
    ADD CONSTRAINT warmup_tasks_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: warmup_tokens warmup_tokens_recipient_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_tokens
    ADD CONSTRAINT warmup_tokens_recipient_account_id_fkey FOREIGN KEY (recipient_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: warmup_tokens warmup_tokens_sender_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_tokens
    ADD CONSTRAINT warmup_tokens_sender_account_id_fkey FOREIGN KEY (sender_account_id) REFERENCES public.email_accounts(id) ON DELETE CASCADE;

--
-- Name: warmup_tokens warmup_tokens_task_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.warmup_tokens
    ADD CONSTRAINT warmup_tokens_task_id_fkey FOREIGN KEY (task_id) REFERENCES public.tasks(id) ON DELETE CASCADE;

--
-- Name: webauthn_credentials webauthn_credentials_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webauthn_credentials
    ADD CONSTRAINT webauthn_credentials_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

--
-- Name: webhook_deliveries webhook_deliveries_endpoint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES public.webhook_endpoints(id) ON DELETE CASCADE;

--
-- Name: webhook_deliveries webhook_deliveries_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: webhook_endpoints webhook_endpoints_organization_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_endpoints
    ADD CONSTRAINT webhook_endpoints_organization_id_fkey FOREIGN KEY (organization_id) REFERENCES public.organizations(id) ON DELETE CASCADE;

--
-- Name: worker_health_samples worker_health_samples_worker_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_health_samples
    ADD CONSTRAINT worker_health_samples_worker_id_fkey FOREIGN KEY (worker_id) REFERENCES public.workers(id) ON DELETE CASCADE;

--
-- Name: worker_profiles worker_profiles_aws_credential_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_profiles
    ADD CONSTRAINT worker_profiles_aws_credential_id_fkey FOREIGN KEY (aws_credential_id) REFERENCES public.aws_credentials(id) ON DELETE RESTRICT;

--
-- Name: worker_tags worker_tags_worker_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.worker_tags
    ADD CONSTRAINT worker_tags_worker_id_fkey FOREIGN KEY (worker_id) REFERENCES public.workers(id) ON DELETE CASCADE;

--
-- Name: workers workers_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workers
    ADD CONSTRAINT workers_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.worker_profiles(id) ON DELETE SET NULL;


--
-- Populate the materialized view. A schema-only pg_dump emits matviews
-- "WITH NO DATA", but the original migration (000052_worker_health) created
-- worker_capacity_view populated, and the runtime refresh path uses
-- REFRESH MATERIALIZED VIEW CONCURRENTLY, which Postgres rejects on a
-- never-populated matview. Refresh once here (non-concurrently, against the
-- unique index defined above) so it starts populated — matching pre-squash
-- behavior. Keep this REFRESH if the baseline is ever regenerated from a
-- schema-only dump, which would otherwise drop it.
--

REFRESH MATERIALIZED VIEW public.worker_capacity_view;
