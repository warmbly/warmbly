// Configuration, settings: the only writable configuration in the product.
// These keys are deliberately disjoint from the environment, so there is no
// precedence to resolve and nothing here can be overwritten at the next boot.

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Check, Plus, Save, Trash2 } from "lucide-react";
import { ErrorState } from "@/components/ErrorState";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { StatusDot } from "@/components/ui/kit";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { TONE_TEXT } from "@/lib/tones";
import { cn } from "@/lib/utils";
import {
    getInstanceSettings,
    putInstanceSettings,
    type InstanceSettings,
    type UsageSettings,
} from "@/lib/api/client/admin/instance";
import { RangeHint, SettingsGroup, SettingsRow } from "./SettingsLayout";

const SETTINGS_KEY = ["admin", "instance", "settings"];

// The backend clamps to the same band; matching it here keeps the operator
// from spending a round-trip to learn the range.
const TTL_MIN_HOURS = 1;
const TTL_MAX_HOURS = 48;

// Sending-domain authentication grace, mirroring internal/app/instancesettings.
const AUTH_GRACE_MIN_HOURS = 1;
const AUTH_GRACE_MAX_HOURS = 720;

// Mailbox sync fair-use bands, mirroring internal/config/constants.go.
const SYNC_FIELDS = [
    {
        key: "backfillDays",
        setting: "backfill_days",
        label: "Import window (days)",
        min: 1,
        max: 730,
        help: "How far back the initial import reaches when a mailbox is connected. Newest mail first.",
    },
    {
        key: "backfillMessages",
        setting: "backfill_messages",
        label: "Import cap (messages per mailbox)",
        min: 1,
        max: 100000,
        help: "The most messages the initial import stores for one mailbox, whatever the window holds.",
    },
    {
        key: "dailyPerMailbox",
        setting: "daily_messages_per_mailbox",
        label: "Daily budget (messages per mailbox)",
        min: 1,
        max: 100000,
        help: "New mail one mailbox may store per UTC day. Over it, mail waits for the next day; replies to the mailbox's own sends have a separate budget of the same size and keep landing.",
    },
    {
        key: "dailyPerOrg",
        setting: "daily_messages_per_org",
        label: "Daily budget (messages per organization)",
        min: 1,
        max: 2000000,
        help: "New plus imported mail across one organization per UTC day.",
    },
] as const;

type SyncFieldKey = (typeof SYNC_FIELDS)[number]["key"];

// Retention windows, mirroring internal/config/constants.go. Each one is also
// how long the personal data in that log is held, so the help text says what
// the data is rather than only what the number does.
const RETENTION_MIN_DAYS = 1;
const RETENTION_MAX_DAYS = 3650;

const RETENTION_FIELDS = [
    {
        key: "engagementDays",
        setting: "engagement_event_days",
        label: "Opens and clicks (days)",
        min: RETENTION_MIN_DAYS,
        help: "Per-event open and click logs, with the client, device and approximate location of each. Campaign counts and routing read a separate summary that is never pruned, so shortening this changes what a contact's timeline can show, not what a campaign does.",
    },
    {
        key: "formDays",
        setting: "form_event_days",
        label: "Form funnel events (days)",
        min: RETENTION_MIN_DAYS,
        help: "Views, starts, field-level drop-off and submissions for hosted forms. Funnel reports range up to 90 days, so anything below that shortens the report too. Submitted contacts are unaffected.",
    },
    {
        key: "auditDays",
        setting: "audit_log_days",
        label: "Audit log (days)",
        min: RETENTION_MIN_DAYS,
        help: "Who did what, from which IP address and user agent, with the change payload. This window is how long that record is held, and it is the one most likely to be set by a retention policy.",
    },
    {
        key: "warmupMailDays",
        setting: "warmup_mail_days",
        label: "Warmup mail in mailboxes (days)",
        min: 3,
        help: "How long warmup mail stays in each mailbox before Warmbly deletes it, wherever the mailbox's filing setting keeps it (Trash on Gmail), with the stored copy of its body. A mailbox may set its own window in its drawer; this is the one every other mailbox follows. The floor leaves room for the engagement and a reply in the thread to finish.",
    },
    {
        key: "warmupEventDays",
        setting: "warmup_event_days",
        label: "Warmup records (days)",
        min: 30,
        help: "Per-message warmup records: tokens, receipts, tampering events, spam reports and the record of which messages belong to a warmup thread. The pool health bands read the last 30 days, which is the floor. The daily sent and received counts behind the analytics are separate and never pruned.",
    },
    {
        key: "campaignLogDays",
        setting: "campaign_log_days",
        label: "Campaign activity feed (days)",
        min: 0,
        help: "Entries in each campaign's activity feed: sends that failed or were deferred, pauses and the reasons for them. Campaign stats, progress and sent mail are separate and unaffected. 0 keeps every entry, which is the default.",
    },
    {
        key: "notificationDays",
        setting: "notification_days",
        label: "Notifications (days)",
        min: 0,
        help: "In-app notifications, read or unread. One still waiting to be emailed is kept until it is sent or skipped. 0 keeps every entry, which is the default.",
    },
    {
        key: "webhookDeliveryDays",
        setting: "webhook_delivery_days",
        label: "Webhook delivery log (days)",
        min: 0,
        help: "Delivered and abandoned webhook deliveries, with the payload and the endpoint's response excerpt. A delivery still being retried is kept until it settles. A delivery past this window can no longer be redelivered from the log. 0 keeps every entry, which is the default.",
    },
] as const;

type RetentionFieldKey = (typeof RETENTION_FIELDS)[number]["key"];

// The two presets are the ends of the band people actually choose between.
const RETENTION_PRESETS = [
    {
        id: "default",
        label: "Defaults",
        description: "365 / 180 / 90 days, warmup 30 / 365, feeds kept",
        values: {
            engagementDays: "365",
            formDays: "180",
            auditDays: "90",
            warmupMailDays: "30",
            warmupEventDays: "365",
            campaignLogDays: "0",
            notificationDays: "0",
            webhookDeliveryDays: "0",
        },
    },
    {
        id: "minimal",
        label: "Minimal retention",
        description: "30 / 30 / 30 days, warmup 7 / 30, feeds 30 / 30 / 30",
        values: {
            engagementDays: "30",
            formDays: "30",
            auditDays: "30",
            warmupMailDays: "7",
            warmupEventDays: "30",
            campaignLogDays: "30",
            notificationDays: "30",
            webhookDeliveryDays: "30",
        },
    },
] as const;

// Engagement-classification windows, mirroring internal/config/constants.go.
// The clock starts when the send is handed to a worker, which is why these are
// larger than "how fast could a person read this": the window also has to
// cover provider queueing and transit before the recipient's gateway sees it.
const MACHINE_WINDOW_MIN_SECONDS = 1;
const MACHINE_WINDOW_MAX_SECONDS = 900;
// The probable window has bounds of its own and reaches a day, because how
// long a security vendor takes to detonate a link is the vendor's property,
// not this instance's.
const PROBABLE_WINDOW_MAX_SECONDS = 86400;

const TRACKING_FIELDS = [
    {
        key: "machineWindowOpen",
        setting: "machine_window_open_seconds",
        label: "Automated open window (seconds)",
        min: MACHINE_WINDOW_MIN_SECONDS,
        max: MACHINE_WINDOW_MAX_SECONDS,
        help: "An open arriving this soon after a send was dispatched is recorded as automated. Raise it when delivery-time scanners are being counted as opens, lower it when recipients who read immediately are being missed.",
    },
    {
        key: "machineWindowClick",
        setting: "machine_window_click_seconds",
        label: "Automated click window (seconds)",
        min: MACHINE_WINDOW_MIN_SECONDS,
        max: MACHINE_WINDOW_MAX_SECONDS,
        help: "The same window for clicks, kept separate because the two mistakes cost different things: a misjudged open loses a metric, a misjudged click loses the automation behind an interested lead.",
    },
    {
        key: "machineWindowProbable",
        setting: "machine_window_probable_seconds",
        label: "Probable-scanner window (seconds)",
        min: MACHINE_WINDOW_MIN_SECONDS,
        max: PROBABLE_WINDOW_MAX_SECONDS,
        help: "Used instead of the two above when the request came from a mail-security network that also renders clicked pages for people, which Proofpoint and Mimecast do through browser isolation. Inside this window the event is classified as the delivery-time scan; past it, as the recipient who got to the mail later. A recipient behind one of those vendors who really does click inside it is recorded as automated, so raise it if their scans still count as engagement and lower it if fast recipients are being missed. It is never applied shorter than the windows above.",
    },
] as const;

type TrackingFieldKey = (typeof TRACKING_FIELDS)[number]["key"];

// Inbox placement test allowance and pacing, mirroring internal/config/constants.go.
const PLACEMENT_FIELDS = [
    {
        key: "testsTrial",
        setting: "tests_per_month_trial",
        label: "Free tests per month on trial",
        min: 1,
        max: 100000,
        help: "How many free tests a workspace without a paid plan may run on the metered panels each calendar month. A trial has no balance to pay from, so it waits for next month after these.",
    },
    {
        key: "testsPaid",
        setting: "tests_per_month_paid",
        label: "Free tests per month on paid plans",
        min: 1,
        max: 100000,
        help: "The same free allowance for a workspace with an active subscription. Past it, a test on the instance panel is charged to the workspace's balance at the placement test price under Usage pricing.",
    },
    {
        key: "seedsPerTest",
        setting: "seeds_per_test",
        label: "Seeds per test",
        min: 1,
        max: 100,
        help: "The most seeds one test sends to, which is also how many sends it takes from the sending mailbox's daily limit. A test is sized down to what the mailbox has left today, and refused below five seeds.",
    },
    {
        key: "spacingSeconds",
        setting: "spacing_seconds",
        label: "Spacing between copies (seconds)",
        min: 5,
        max: 600,
        help: "The gap between two copies from one mailbox, jittered, so a test never leaves as a burst. A test someone starts as quick uses the smaller of this and 8 seconds.",
    },
    {
        key: "batchSendersMax",
        setting: "batch_senders_max",
        label: "Most senders in one batch",
        min: 1,
        max: 100000,
        help: "The largest sender list one placement batch may hold. It limits how big a batch can be, not how many senders run at once, so it can sit well above any fleet a workspace runs.",
    },
    {
        key: "batchSenderConcurrency",
        setting: "batch_sender_concurrency",
        label: "Batch senders sending at once",
        min: 1,
        max: 500,
        help: "How many of one workspace's batch senders may be sending their copies at the same time, across all its batches. The next sender starts when one has sent every copy. Each mailbox still keeps its own daily limit and spacing.",
    },
    {
        key: "batchInstanceConcurrency",
        setting: "batch_instance_concurrency",
        label: "Batch senders sending at once, instance-wide",
        min: 1,
        max: 5000,
        help: "The same limit across every workspace together. It bounds how much batch mail the instance seed panel receives at once, so a seed never takes in enough in an hour to trip the sync flood rule that deactivates a mailbox.",
    },
    {
        key: "batchStartsPerMinute",
        setting: "batch_starts_per_minute",
        label: "Batch senders started per minute",
        min: 1,
        max: 600,
        help: "How fast one batch starts its senders, so a large batch ramps up instead of starting its whole concurrency at once.",
    },
] as const;

type PlacementFieldKey = (typeof PLACEMENT_FIELDS)[number]["key"];

// A backend from before the placement section omits it; the form shows the compiled defaults.
const PLACEMENT_DEFAULTS: InstanceSettings["placement"] = {
    tests_per_month_trial: 3,
    tests_per_month_paid: 40,
    seeds_per_test: 20,
    spacing_seconds: 60,
    batch_senders_max: 10000,
    batch_sender_concurrency: 20,
    batch_instance_concurrency: 200,
    batch_starts_per_minute: 10,
};

// Usage pricing bounds and compiled defaults, mirroring internal/app/instancesettings/usage.go.
const MARKUP_MIN = 0;
const MARKUP_MAX = 500;
const ANY_MODEL = "*";

// A backend from before the balance omits the usage section; the form shows the compiled defaults.
const USAGE_DEFAULTS: UsageSettings = {
    markup_percent: 25,
    models: [
        { model: "claude-sonnet-4", input_per_mtok: 3, output_per_mtok: 15 },
        { model: "claude-haiku-4", input_per_mtok: 1, output_per_mtok: 5 },
        { model: "claude-3-5-haiku", input_per_mtok: 0.8, output_per_mtok: 4 },
        { model: "gpt-6-luna", input_per_mtok: 0.1, output_per_mtok: 0.5 },
        { model: ANY_MODEL, input_per_mtok: 3, output_per_mtok: 15 },
    ],
    units: {
        web_search: 0.01,
        placement_test: 0.4,
        lead_website_read: 0.016,
        lead_person_email: 0.016,
        lead_search_page: 0.032,
    },
};

// Known units first, in this order; any other unit the backend prices is listed after them by key.
const UNIT_LABELS: Record<string, { label: string; help: string }> = {
    web_search: {
        label: "Web search",
        help: "One web search made by contact research, an AI variable, or an AI step in a campaign or automation.",
    },
    placement_test: {
        label: "Placement test",
        help: "One inbox placement test past the workspace's free tests for the month. The workspace agrees to the price before each paid test, and a test that delivers no copy is refunded. 0 turns paid tests off, so a workspace waits for next month instead.",
    },
    lead_website_read: {
        label: "Lead website read",
        help: "The lead finder reading one business's website, including the people it names and their addresses. A site that never answers is refunded.",
    },
    lead_person_email: {
        label: "Lead person email",
        help: "Finding one named person's email in the lead finder on request. A search that finds no address is refunded.",
    },
    lead_search_page: {
        label: "Lead search page",
        help: "One page of a lead finder search through a source this instance pays for. The default is Google Places' Text Search list price; set it to what your provider charges. A workspace using its own key is not charged.",
    },
};

interface ModelRow {
    // Stable React key; a model name is edited in place, so it cannot be the key.
    id: string;
    model: string;
    input: string;
    output: string;
}

interface UsageForm {
    markup: string;
    models: ModelRow[];
    units: Record<string, string>;
}

let modelRowSeq = 0;
function nextModelRowId(): string {
    modelRowSeq += 1;
    return `model-${modelRowSeq}`;
}

function toUsageForm(u: UsageSettings | undefined): UsageForm {
    const usage = u ?? USAGE_DEFAULTS;
    const models = usage.models.map((m) => ({
        id: nextModelRowId(),
        model: m.model,
        input: String(m.input_per_mtok),
        output: String(m.output_per_mtok),
    }));
    // The "*" row is required; the backend adds it, so this only covers an odd document.
    if (!models.some((m) => m.model === ANY_MODEL)) {
        const fallback = USAGE_DEFAULTS.models[USAGE_DEFAULTS.models.length - 1];
        models.push({
            id: nextModelRowId(),
            model: ANY_MODEL,
            input: String(fallback.input_per_mtok),
            output: String(fallback.output_per_mtok),
        });
    }
    const units: Record<string, string> = {};
    for (const [k, v] of Object.entries({ ...USAGE_DEFAULTS.units, ...usage.units })) {
        units[k] = String(v);
    }
    return { markup: String(usage.markup_percent ?? USAGE_DEFAULTS.markup_percent), models, units };
}

// Compared without row ids, so reseeding the form does not read as an edit.
function usageSignature(u: UsageForm): string {
    return JSON.stringify({
        markup: u.markup,
        models: u.models.map((m) => [m.model, m.input, m.output]),
        units: Object.entries(u.units).sort(([a], [b]) => a.localeCompare(b)),
    });
}

function unitKeys(units: Record<string, string>): string[] {
    const known = Object.keys(UNIT_LABELS).filter((k) => k in units);
    const other = Object.keys(units)
        .filter((k) => !(k in UNIT_LABELS))
        .sort();
    return [...known, ...other];
}

function priceValid(raw: string): boolean {
    const n = Number(raw);
    return raw.trim() !== "" && Number.isFinite(n) && n >= 0;
}

function modelNameError(rows: ModelRow[], row: ModelRow): string | undefined {
    const name = row.model.trim().toLowerCase();
    if (name === "") return "Enter a model name, or remove the row.";
    if (rows.filter((r) => r.model.trim().toLowerCase() === name).length > 1) {
        return "Each model is listed once.";
    }
    return undefined;
}

// What a workspace is charged for a provider price after the markup.
function withMarkup(raw: string, markup: string): string | null {
    const price = Number(raw);
    const pct = Number(markup);
    if (!priceValid(raw) || !Number.isFinite(pct)) return null;
    return formatUSD((price * (100 + pct)) / 100);
}

function formatUSD(n: number): string {
    return `$${n.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 4 })}`;
}

interface FormState {
    linksEnabled: boolean;
    ttlHours: string;
    allowInvitedSignup: boolean;
    sync: Record<SyncFieldKey, string>;
    retention: Record<RetentionFieldKey, string>;
    tracking: Record<TrackingFieldKey, string>;
    placement: Record<PlacementFieldKey, string>;
    enforceDomainAuth: boolean;
    authGraceHours: string;
    autoVendorReconnect: boolean;
    usage: UsageForm;
}

function toForm(s: InstanceSettings): FormState {
    const placement = s.placement ?? PLACEMENT_DEFAULTS;
    return {
        linksEnabled: s.invitations.links_enabled,
        ttlHours: String(s.invitations.ttl_hours),
        allowInvitedSignup: s.access.allow_invited_signup,
        sync: {
            backfillDays: String(s.sync.backfill_days),
            backfillMessages: String(s.sync.backfill_messages),
            dailyPerMailbox: String(s.sync.daily_messages_per_mailbox),
            dailyPerOrg: String(s.sync.daily_messages_per_org),
        },
        retention: {
            engagementDays: String(s.retention.engagement_event_days),
            formDays: String(s.retention.form_event_days),
            auditDays: String(s.retention.audit_log_days),
            warmupMailDays: String(s.retention.warmup_mail_days),
            warmupEventDays: String(s.retention.warmup_event_days),
            campaignLogDays: String(s.retention.campaign_log_days ?? 0),
            notificationDays: String(s.retention.notification_days ?? 0),
            webhookDeliveryDays: String(s.retention.webhook_delivery_days ?? 0),
        },
        tracking: {
            machineWindowOpen: String(s.tracking.machine_window_open_seconds),
            machineWindowClick: String(s.tracking.machine_window_click_seconds),
            machineWindowProbable: String(s.tracking.machine_window_probable_seconds),
        },
        placement: {
            testsTrial: String(placement.tests_per_month_trial),
            testsPaid: String(placement.tests_per_month_paid),
            seedsPerTest: String(placement.seeds_per_test),
            spacingSeconds: String(placement.spacing_seconds),
            batchSendersMax: String(placement.batch_senders_max ?? PLACEMENT_DEFAULTS.batch_senders_max),
            batchSenderConcurrency: String(placement.batch_sender_concurrency ?? PLACEMENT_DEFAULTS.batch_sender_concurrency),
            batchInstanceConcurrency: String(placement.batch_instance_concurrency ?? PLACEMENT_DEFAULTS.batch_instance_concurrency),
            batchStartsPerMinute: String(placement.batch_starts_per_minute ?? PLACEMENT_DEFAULTS.batch_starts_per_minute),
        },
        enforceDomainAuth: s.deliverability.enforce_domain_auth,
        authGraceHours: String(s.deliverability.auth_grace_hours),
        autoVendorReconnect: s.mailbox_signin?.auto_vendor_reconnect ?? true,
        usage: toUsageForm(s.usage),
    };
}

function syncFieldValid(raw: string, min: number, max: number): boolean {
    const n = Number(raw);
    return raw.trim() !== "" && Number.isInteger(n) && n >= min && n <= max;
}

interface SettingsTabProps {
    // Reported on every change so the page can confirm before a tab switch or
    // a navigation throws the edits away.
    onDirtyChange?: (dirty: boolean) => void;
    onSwitchTab?: (tab: "environment" | "limits") => void;
}

export function SettingsTab({ onDirtyChange, onSwitchTab }: SettingsTabProps) {
    const qc = useQueryClient();
    const [form, setForm] = useState<FormState | null>(null);

    const settingsQ = useQuery({
        queryKey: SETTINGS_KEY,
        queryFn: getInstanceSettings,
        retry: false,
    });

    // Reseed whenever a fresh document arrives so a background refetch does
    // not silently keep an operator editing a stale form.
    useEffect(() => {
        if (settingsQ.data) setForm(toForm(settingsQ.data));
    }, [settingsQ.data]);

    const saveMut = useMutation({
        mutationFn: (body: InstanceSettings) => putInstanceSettings(body),
        onSuccess: (saved) => {
            qc.setQueryData(SETTINGS_KEY, saved);
            setForm(toForm(saved));
            toast.success("Instance settings saved");
        },
        onError: (err: Error) => toast.error(err.message || "Could not save settings"),
    });

    const server = settingsQ.data;
    const syncDirty =
        !!server &&
        !!form &&
        SYNC_FIELDS.some((f) => form.sync[f.key] !== String(server.sync[f.setting]));
    const retentionDirty =
        !!server &&
        !!form &&
        RETENTION_FIELDS.some(
            (f) => form.retention[f.key] !== String(server.retention[f.setting] ?? 0),
        );
    const trackingDirty =
        !!server &&
        !!form &&
        TRACKING_FIELDS.some((f) => form.tracking[f.key] !== String(server.tracking[f.setting]));
    const placementDirty =
        !!server &&
        !!form &&
        PLACEMENT_FIELDS.some(
            (f) =>
                form.placement[f.key] !== String(server.placement?.[f.setting] ?? PLACEMENT_DEFAULTS[f.setting]),
        );
    const usageDirty =
        !!server && !!form && usageSignature(form.usage) !== usageSignature(toUsageForm(server.usage));
    const dirty =
        !!server &&
        !!form &&
        (form.linksEnabled !== server.invitations.links_enabled ||
            form.ttlHours !== String(server.invitations.ttl_hours) ||
            form.allowInvitedSignup !== server.access.allow_invited_signup ||
            form.enforceDomainAuth !== server.deliverability.enforce_domain_auth ||
            form.authGraceHours !== String(server.deliverability.auth_grace_hours) ||
            form.autoVendorReconnect !== (server.mailbox_signin?.auto_vendor_reconnect ?? true) ||
            retentionDirty ||
            trackingDirty ||
            placementDirty ||
            usageDirty ||
            syncDirty);

    useEffect(() => {
        onDirtyChange?.(dirty);
        return () => onDirtyChange?.(false);
    }, [dirty, onDirtyChange]);
    const syncValid =
        form !== null && SYNC_FIELDS.every((f) => syncFieldValid(form.sync[f.key], f.min, f.max));
    const retentionValid =
        form !== null &&
        RETENTION_FIELDS.every((f) => syncFieldValid(form.retention[f.key], f.min, RETENTION_MAX_DAYS));

    const trackingValid =
        form !== null &&
        TRACKING_FIELDS.every((f) => syncFieldValid(form.tracking[f.key], f.min, f.max));

    const placementValid =
        form !== null &&
        PLACEMENT_FIELDS.every((f) => syncFieldValid(form.placement[f.key], f.min, f.max));

    const usageValid =
        form !== null &&
        syncFieldValid(form.usage.markup, MARKUP_MIN, MARKUP_MAX) &&
        form.usage.models.every(
            (m) =>
                !modelNameError(form.usage.models, m) && priceValid(m.input) && priceValid(m.output),
        ) &&
        Object.values(form.usage.units).every(priceValid);

    const authGrace = form ? Number(form.authGraceHours) : NaN;
    const authGraceValid =
        form !== null &&
        form.authGraceHours.trim() !== "" &&
        Number.isInteger(authGrace) &&
        authGrace >= AUTH_GRACE_MIN_HOURS &&
        authGrace <= AUTH_GRACE_MAX_HOURS;

    const ttl = form ? Number(form.ttlHours) : NaN;
    const ttlValid =
        form !== null &&
        form.ttlHours.trim() !== "" &&
        Number.isInteger(ttl) &&
        ttl >= TTL_MIN_HOURS &&
        ttl <= TTL_MAX_HOURS;

    function save() {
        if (!form) return;
        if (!ttlValid) {
            toast.error(
                `Invitation validity must be a whole number of hours between ${TTL_MIN_HOURS} and ${TTL_MAX_HOURS}`,
            );
            return;
        }
        if (!syncValid) {
            toast.error("Every sync budget must be a whole number inside its range");
            return;
        }
        if (!retentionValid) {
            toast.error(
                `Every retention window must be a whole number of days up to ${RETENTION_MAX_DAYS.toLocaleString()}, and not below the floor shown under it`,
            );
            return;
        }
        if (!trackingValid) {
            toast.error(
                "Every automated-engagement window must be a whole number of seconds inside the range shown under it",
            );
            return;
        }
        if (!placementValid) {
            toast.error("Every placement test setting must be a whole number inside the range shown under it");
            return;
        }
        if (!usageValid) {
            toast.error(
                `The markup must be a whole number from ${MARKUP_MIN} to ${MARKUP_MAX}, and every model needs a unique name and prices of zero or more`,
            );
            return;
        }
        if (!authGraceValid) {
            toast.error(
                `The authentication grace period must be a whole number of hours between ${AUTH_GRACE_MIN_HOURS} and ${AUTH_GRACE_MAX_HOURS}`,
            );
            return;
        }
        saveMut.mutate({
            invitations: { links_enabled: form.linksEnabled, ttl_hours: ttl },
            access: { allow_invited_signup: form.allowInvitedSignup },
            sync: {
                backfill_days: Number(form.sync.backfillDays),
                backfill_messages: Number(form.sync.backfillMessages),
                daily_messages_per_mailbox: Number(form.sync.dailyPerMailbox),
                daily_messages_per_org: Number(form.sync.dailyPerOrg),
            },
            retention: {
                engagement_event_days: Number(form.retention.engagementDays),
                form_event_days: Number(form.retention.formDays),
                audit_log_days: Number(form.retention.auditDays),
                warmup_mail_days: Number(form.retention.warmupMailDays),
                warmup_event_days: Number(form.retention.warmupEventDays),
                campaign_log_days: Number(form.retention.campaignLogDays),
                notification_days: Number(form.retention.notificationDays),
                webhook_delivery_days: Number(form.retention.webhookDeliveryDays),
            },
            tracking: {
                machine_window_open_seconds: Number(form.tracking.machineWindowOpen),
                machine_window_click_seconds: Number(form.tracking.machineWindowClick),
                machine_window_probable_seconds: Number(form.tracking.machineWindowProbable),
            },
            deliverability: {
                enforce_domain_auth: form.enforceDomainAuth,
                auth_grace_hours: authGrace,
            },
            mailbox_signin: { auto_vendor_reconnect: form.autoVendorReconnect },
            placement: {
                tests_per_month_trial: Number(form.placement.testsTrial),
                tests_per_month_paid: Number(form.placement.testsPaid),
                seeds_per_test: Number(form.placement.seedsPerTest),
                spacing_seconds: Number(form.placement.spacingSeconds),
                batch_senders_max: Number(form.placement.batchSendersMax),
                batch_sender_concurrency: Number(form.placement.batchSenderConcurrency),
                batch_instance_concurrency: Number(form.placement.batchInstanceConcurrency),
                batch_starts_per_minute: Number(form.placement.batchStartsPerMinute),
            },
            usage: {
                markup_percent: Number(form.usage.markup),
                models: form.usage.models.map((m) => ({
                    model: m.model.trim(),
                    input_per_mtok: Number(m.input),
                    output_per_mtok: Number(m.output),
                })),
                units: Object.fromEntries(
                    Object.entries(form.usage.units).map(([k, v]) => [k, Number(v)]),
                ),
            },
        });
    }

    return (
        <div>
            <div className="mb-8 flex flex-wrap items-start justify-between gap-3">
                <p className="max-w-2xl text-[13px] leading-relaxed text-muted-foreground">
                    Stored in the database and never read from the environment. Everything the
                    environment owns is on the Environment tab.
                </p>
                <Button
                    size="sm"
                    onClick={save}
                    disabled={!dirty || saveMut.isPending || !form}
                >
                    <Save />
                    {saveMut.isPending ? "Saving..." : "Save changes"}
                </Button>
            </div>

            {settingsQ.isLoading && (
                <div className="space-y-10">
                    {[0, 1, 2].map((i) => (
                        <div key={i}>
                            <Skeleton className="mb-3 h-4 w-32" />
                            <Skeleton className="h-32 w-full rounded-lg" />
                        </div>
                    ))}
                </div>
            )}

            {settingsQ.isError && (
                <ErrorState
                    error={settingsQ.error}
                    title="Could not load instance settings"
                    onRetry={() => settingsQ.refetch()}
                />
            )}

            {form && (
                <div>
                    <SettingsGroup
                        title="Invitations"
                        description="How people are brought into a workspace from Settings, Members in the dashboard."
                    >
                        <SettingsRow
                            label="Invitation links"
                            description="Show a copyable link next to each pending invitation. Leave this on when the platform mail transport does not deliver, otherwise an invited person never receives anything."
                        >
                            <Switch
                                checked={form.linksEnabled}
                                onCheckedChange={(v) => setForm({ ...form, linksEnabled: v })}
                            />
                        </SettingsRow>
                        <NumberRow
                            id="ttl-hours"
                            label="Invitation validity (hours)"
                            help="Existing invitations keep the expiry they were issued with."
                            range={`Between ${TTL_MIN_HOURS} and ${TTL_MAX_HOURS} hours (2 days).`}
                            value={form.ttlHours}
                            onChange={(v) => setForm({ ...form, ttlHours: v })}
                            valid={ttlValid}
                            error={`Enter a whole number of hours between ${TTL_MIN_HOURS} and ${TTL_MAX_HOURS}.`}
                        />
                    </SettingsGroup>

                    <SettingsGroup
                        title="Access"
                        description={
                            <>
                                Who may create an account on this instance. The registration mode
                                itself is owned by the environment and is listed under{" "}
                                <TabLink onClick={() => onSwitchTab?.("environment")}>
                                    Environment
                                </TabLink>
                                .
                            </>
                        }
                    >
                        <SettingsRow
                            label="Allow invited sign-up"
                            description="Someone holding a valid invitation can create an account even though open sign-ups are closed. Turning this off means only existing accounts can sign in."
                        >
                            <Switch
                                checked={form.allowInvitedSignup}
                                onCheckedChange={(v) => setForm({ ...form, allowInvitedSignup: v })}
                            />
                        </SettingsRow>
                    </SettingsGroup>

                    <SettingsGroup
                        title="Mailbox sync fair use"
                        description={
                            <>
                                What a connected mailbox imports and how much new mail it may
                                store. Mail over a budget waits and is picked up when the window
                                rolls; nothing is dropped, and replies to the mailbox&apos;s own
                                outreach are never held. Changes apply the next time a mailbox is
                                loaded onto a worker (within a few minutes). The fixed pacing
                                numbers are listed under{" "}
                                <TabLink onClick={() => onSwitchTab?.("limits")}>Limits</TabLink>.
                            </>
                        }
                    >
                        {SYNC_FIELDS.map((f) => (
                            <NumberRow
                                key={f.key}
                                id={`sync-${f.key}`}
                                label={f.label}
                                help={f.help}
                                range={`Between ${f.min.toLocaleString()} and ${f.max.toLocaleString()}.`}
                                value={form.sync[f.key]}
                                onChange={(v) => setForm({ ...form, sync: { ...form.sync, [f.key]: v } })}
                                valid={syncFieldValid(form.sync[f.key], f.min, f.max)}
                                error={`Enter a whole number between ${f.min.toLocaleString()} and ${f.max.toLocaleString()}.`}
                            />
                        ))}
                    </SettingsGroup>

                    <SettingsGroup
                        title="Data retention"
                        description="How long event-level history is kept on this instance. Every window below is also how long the personal data in that log is held, so these are the settings a retention or privacy policy applies to. A sweep runs a few times a day and reads these values each pass, so a change takes effect without a restart. Deletion is permanent: shortening a window removes what already sits outside it on the next sweep."
                    >
                        <SettingsRow
                            label="Presets"
                            description="Fill every window below from a common choice."
                            className="sm:flex-col sm:items-stretch sm:gap-3"
                        >
                            <div className="flex flex-wrap gap-1.5">
                                {RETENTION_PRESETS.map((preset) => {
                                    const active = RETENTION_FIELDS.every(
                                        (f) => form.retention[f.key] === preset.values[f.key],
                                    );
                                    return (
                                        <Button
                                            key={preset.id}
                                            type="button"
                                            size="sm"
                                            variant="outline"
                                            aria-pressed={active}
                                            className={cn(
                                                active &&
                                                    "border-[color-mix(in_oklab,var(--admin-accent)_40%,transparent)] bg-[var(--admin-accent-weak)] text-[var(--admin-accent-strong)] hover:bg-[var(--admin-accent-soft)] dark:bg-[var(--admin-accent-weak)] dark:hover:bg-[var(--admin-accent-soft)]",
                                            )}
                                            onClick={() =>
                                                setForm({
                                                    ...form,
                                                    retention: { ...preset.values },
                                                })
                                            }
                                        >
                                            {active && <Check />}
                                            {preset.label}
                                            <span className="text-[11px] font-normal opacity-70">
                                                {preset.description}
                                            </span>
                                        </Button>
                                    );
                                })}
                            </div>
                        </SettingsRow>
                        {RETENTION_FIELDS.map((f) => (
                            <NumberRow
                                key={f.key}
                                id={`retention-${f.key}`}
                                label={f.label}
                                help={f.help}
                                range={`Between ${f.min} and ${RETENTION_MAX_DAYS.toLocaleString()} days.`}
                                value={form.retention[f.key]}
                                onChange={(v) =>
                                    setForm({ ...form, retention: { ...form.retention, [f.key]: v } })
                                }
                                valid={syncFieldValid(form.retention[f.key], f.min, RETENTION_MAX_DAYS)}
                                error={`Enter a whole number of days between ${f.min} and ${RETENTION_MAX_DAYS.toLocaleString()}.`}
                            />
                        ))}
                    </SettingsGroup>

                    <SettingsGroup
                        title="Automated engagement"
                        description="Security gateways fetch the tracking pixel and walk every link when a message arrives, using an ordinary browser's user agent. An open or click landing inside these windows is recorded as automated: still kept as delivery evidence and still shown on the timeline, but it does not count as engagement, fire a branch or automation, or send a webhook. Nothing is discarded either way. The clock starts when the send is handed to a worker, so the window also covers the provider's queue and the transit to the recipient. A network that only ever filters mail is matched by name and is not bounded by time; one that can also carry a person gets the probable window below. A change applies within a minute and only to events recorded after it: opens and clicks already stored keep the label they were given when they arrived."
                    >
                        {TRACKING_FIELDS.map((f) => (
                            <NumberRow
                                key={f.key}
                                id={`tracking-${f.key}`}
                                label={f.label}
                                help={f.help}
                                range={`Between ${f.min} and ${f.max.toLocaleString()} seconds.`}
                                value={form.tracking[f.key]}
                                onChange={(v) =>
                                    setForm({ ...form, tracking: { ...form.tracking, [f.key]: v } })
                                }
                                valid={syncFieldValid(form.tracking[f.key], f.min, f.max)}
                                error={`Enter a whole number between ${f.min} and ${f.max.toLocaleString()}.`}
                            />
                        ))}
                    </SettingsGroup>

                    <SettingsGroup
                        title="Inbox placement tests"
                        description="A placement test sends one copy of a template to each seed on a panel and reports where it landed. The monthly allowances count tests on the instance panel and on Warmbly Cloud's; tests on a workspace's own seed inboxes are never counted, and a self-hosted instance does not meter tests at all. A tracking comparison counts as two tests. The seeds themselves are managed on the Seed panel page."
                    >
                        {PLACEMENT_FIELDS.map((f) => (
                            <NumberRow
                                key={f.key}
                                id={`placement-${f.key}`}
                                label={f.label}
                                help={f.help}
                                range={`Between ${f.min.toLocaleString()} and ${f.max.toLocaleString()}.`}
                                value={form.placement[f.key]}
                                onChange={(v) =>
                                    setForm({ ...form, placement: { ...form.placement, [f.key]: v } })
                                }
                                valid={syncFieldValid(form.placement[f.key], f.min, f.max)}
                                error={`Enter a whole number between ${f.min.toLocaleString()} and ${f.max.toLocaleString()}.`}
                            />
                        ))}
                    </SettingsGroup>

                    <UsagePricingGroup
                        usage={form.usage}
                        onChange={(usage) => setForm({ ...form, usage })}
                    />

                    <SettingsGroup
                        title="Sending-domain authentication"
                        description="Gmail, Yahoo, and Outlook reject or spam-filter mail from a domain without SPF and DMARC, and one unauthenticated sender damages the reputation of every mailbox in the shared warmup pool. Warmbly checks each sending domain daily and can stop cold sending and warmup from a domain that keeps failing. The grace period is how long a domain may keep failing first, so a DNS outage cannot stop a customer's campaigns and the owner is warned throughout it."
                    >
                        <SettingsRow
                            label="Stop sending from unauthenticated domains"
                            description="Off keeps the check informational: domains are still checked, shown on the mailbox, and raised by the advisor, but nothing is ever blocked."
                        >
                            <Switch
                                checked={form.enforceDomainAuth}
                                onCheckedChange={(v) => setForm({ ...form, enforceDomainAuth: v })}
                            />
                        </SettingsRow>
                        <NumberRow
                            id="auth-grace-hours"
                            label="Grace period (hours)"
                            help="How long a domain must stay failing before its mailboxes stop sending."
                            range={`Between ${AUTH_GRACE_MIN_HOURS} and ${AUTH_GRACE_MAX_HOURS.toLocaleString()}.`}
                            value={form.authGraceHours}
                            onChange={(v) => setForm({ ...form, authGraceHours: v })}
                            valid={authGraceValid}
                            disabled={!form.enforceDomainAuth}
                            error={`Enter a whole number between ${AUTH_GRACE_MIN_HOURS} and ${AUTH_GRACE_MAX_HOURS.toLocaleString()}.`}
                        />
                    </SettingsGroup>

                    <SettingsGroup
                        title="Mailbox sign-in"
                        description="Google mailboxes imported from InboxKit or Zapmail can be reconnected in place with full mail access by the vendor itself: it marks Warmbly trusted on the domain and signs in as each mailbox. The mailbox, its history, campaigns and warmup stay the same, and one that cannot be reconnected keeps working on its current sign-in."
                    >
                        <SettingsRow
                            label="Reconnect vendor mailboxes automatically"
                            description="On, Warmbly asks the vendor on its own for each linked Google Workspace mailbox on an app password, the older Google sign-in or a legacy domain grant, at most once a week per mailbox. Off leaves it to a member to click Reconnect."
                        >
                            <Switch
                                checked={form.autoVendorReconnect}
                                onCheckedChange={(v) => setForm({ ...form, autoVendorReconnect: v })}
                            />
                        </SettingsRow>
                    </SettingsGroup>
                </div>
            )}

            {form && dirty && (
                <div className="sticky bottom-4 z-10 mt-8 flex flex-wrap items-center gap-2 rounded-lg border border-border bg-popover px-3 py-2 shadow-popover">
                    <span className="mr-auto flex items-center gap-2 text-xs text-muted-foreground">
                        <StatusDot tone="warning" />
                        Saving records the change in the admin audit log.
                    </span>
                    <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => server && setForm(toForm(server))}
                        disabled={saveMut.isPending}
                    >
                        Discard
                    </Button>
                    <Button size="sm" onClick={save} disabled={saveMut.isPending}>
                        <Save />
                        {saveMut.isPending ? "Saving..." : "Save changes"}
                    </Button>
                </div>
            )}
        </div>
    );
}

// The price list a workspace's balance is charged from: provider price plus the markup.
function UsagePricingGroup({
    usage,
    onChange,
}: {
    usage: UsageForm;
    onChange: (u: UsageForm) => void;
}) {
    const markupValid = syncFieldValid(usage.markup, MARKUP_MIN, MARKUP_MAX);
    const setModel = (id: string, patch: Partial<ModelRow>) =>
        onChange({
            ...usage,
            models: usage.models.map((m) => (m.id === id ? { ...m, ...patch } : m)),
        });
    const addModel = () => {
        // New rows go above the "*" row, which reads as the fallback at the end.
        const rows = usage.models.filter((m) => m.model !== ANY_MODEL);
        const any = usage.models.filter((m) => m.model === ANY_MODEL);
        onChange({
            ...usage,
            models: [...rows, { id: nextModelRowId(), model: "", input: "0", output: "0" }, ...any],
        });
    };

    return (
        <SettingsGroup
            title="Usage pricing"
            description="What a workspace's balance is charged for AI and other metered work: the provider's own price for what a call used, plus the markup. Prices are in US dollars before the markup. A change applies to charges made after it. A self-hosted instance with billing off charges nothing, so these prices do nothing there."
            actions={
                <Button type="button" size="sm" variant="outline" onClick={addModel}>
                    <Plus />
                    Add model
                </Button>
            }
        >
            <NumberRow
                id="usage-markup"
                label="Markup (%)"
                help="Added on top of every provider price below. 25 charges $1.25 for each $1.00 of provider cost."
                range={`Between ${MARKUP_MIN} and ${MARKUP_MAX}.`}
                value={usage.markup}
                onChange={(v) => onChange({ ...usage, markup: v })}
                valid={markupValid}
                error={`Enter a whole number between ${MARKUP_MIN} and ${MARKUP_MAX}.`}
            />
            <div className="px-4 py-3.5">
                <div className="text-[13px] font-medium text-foreground">Models</div>
                <div className="mt-0.5 text-[12.5px] leading-relaxed text-muted-foreground">
                    Dollars per million input and output tokens. A model is priced by the row with
                    its exact name, then by the longest row its name starts with, then by{" "}
                    <code className="font-mono text-[12px]">*</code>, which prices every model
                    without a row of its own and cannot be removed.
                </div>
                <div className="mt-3 hidden grid-cols-[minmax(0,1fr)_7rem_7rem_1.75rem] gap-2 text-[11px] font-medium uppercase tracking-wide text-subtle-foreground sm:grid">
                    <span>Model</span>
                    <span className="text-right">Input / Mtok</span>
                    <span className="text-right">Output / Mtok</span>
                    <span />
                </div>
                <div className="mt-2 space-y-3 sm:mt-1.5 sm:space-y-2">
                    {usage.models.map((m) => {
                        const isAny = m.model === ANY_MODEL;
                        const nameError = isAny ? undefined : modelNameError(usage.models, m);
                        const inputOk = priceValid(m.input);
                        const outputOk = priceValid(m.output);
                        const chargedIn = withMarkup(m.input, usage.markup);
                        const chargedOut = withMarkup(m.output, usage.markup);
                        return (
                            <div key={m.id}>
                                <div className="grid grid-cols-[minmax(0,1fr)_1.75rem] gap-2 sm:grid-cols-[minmax(0,1fr)_7rem_7rem_1.75rem]">
                                    <Input
                                        aria-label="Model name"
                                        placeholder="e.g. claude-sonnet-4"
                                        autoComplete="off"
                                        spellCheck={false}
                                        value={m.model}
                                        readOnly={isAny}
                                        onChange={(e) => setModel(m.id, { model: e.target.value })}
                                        aria-invalid={!!nameError}
                                        className={cn("font-mono text-[12.5px]", isAny && "text-muted-foreground")}
                                    />
                                    <Button
                                        type="button"
                                        size="icon-sm"
                                        variant="ghost"
                                        aria-label={isAny ? "The * row cannot be removed" : `Remove ${m.model || "model"}`}
                                        title={isAny ? "Prices every model without a row; required" : "Remove"}
                                        disabled={isAny}
                                        className="hover:bg-destructive/10 hover:text-destructive sm:order-last"
                                        onClick={() =>
                                            onChange({
                                                ...usage,
                                                models: usage.models.filter((r) => r.id !== m.id),
                                            })
                                        }
                                    >
                                        <Trash2 className="size-3.5" />
                                    </Button>
                                    <div className="col-span-2 grid grid-cols-2 gap-2 sm:col-span-2">
                                        <Input
                                            aria-label={`Input price per million tokens for ${m.model || "model"}`}
                                            inputMode="decimal"
                                            autoComplete="off"
                                            value={m.input}
                                            onChange={(e) => setModel(m.id, { input: e.target.value })}
                                            aria-invalid={!inputOk}
                                            className="text-right tabular-nums"
                                        />
                                        <Input
                                            aria-label={`Output price per million tokens for ${m.model || "model"}`}
                                            inputMode="decimal"
                                            autoComplete="off"
                                            value={m.output}
                                            onChange={(e) => setModel(m.id, { output: e.target.value })}
                                            aria-invalid={!outputOk}
                                            className="text-right tabular-nums"
                                        />
                                    </div>
                                </div>
                                <div className="mt-1 text-xs text-subtle-foreground sm:text-right sm:pr-9">
                                    {chargedIn && chargedOut
                                        ? `charged ${chargedIn} / ${chargedOut} per million tokens`
                                        : null}
                                </div>
                                {(nameError || !inputOk || !outputOk) && (
                                    <p className={cn("mt-0.5 text-xs", TONE_TEXT.danger)}>
                                        {nameError ?? "Enter a price of zero or more, in dollars."}
                                    </p>
                                )}
                            </div>
                        );
                    })}
                </div>
            </div>
            {unitKeys(usage.units).map((key) => {
                const raw = usage.units[key];
                const charged = withMarkup(raw, usage.markup);
                const meta = UNIT_LABELS[key];
                return (
                    <NumberRow
                        key={key}
                        id={`usage-unit-${key}`}
                        label={`${meta?.label ?? key} ($ per unit)`}
                        help={meta?.help ?? `The provider price of one ${key.replaceAll("_", " ")}.`}
                        range={charged ? `A workspace is charged ${charged}.` : "Zero or more, in dollars."}
                        value={raw}
                        onChange={(v) => onChange({ ...usage, units: { ...usage.units, [key]: v } })}
                        valid={priceValid(raw)}
                        error="Enter a price of zero or more, in dollars."
                        inputMode="decimal"
                    />
                );
            })}
        </SettingsGroup>
    );
}

// One numeric setting: the field sits on the right, the range under the help.
function NumberRow({
    id,
    label,
    help,
    range,
    value,
    onChange,
    valid,
    error,
    disabled,
    inputMode = "numeric",
}: {
    id: string;
    label: string;
    help: string;
    range: string;
    value: string;
    onChange: (v: string) => void;
    valid: boolean;
    error: string;
    disabled?: boolean;
    inputMode?: "numeric" | "decimal";
}) {
    return (
        <SettingsRow
            label={label}
            htmlFor={id}
            description={
                <>
                    {help} <RangeHint>{range}</RangeHint>
                </>
            }
            error={!valid ? error : undefined}
        >
            {/* Text, not number: the native spinner is not ours, and the value is already validated as a string. */}
            <Input
                id={id}
                type="text"
                inputMode={inputMode}
                autoComplete="off"
                value={value}
                onChange={(e) => onChange(e.target.value)}
                aria-invalid={!valid}
                disabled={disabled}
                className="w-full text-right tabular-nums sm:w-28"
            />
        </SettingsRow>
    );
}

// An inline link inside descriptive copy that switches tabs instead of leaving the page.
function TabLink({ onClick, children }: { onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            type="button"
            onClick={onClick}
            className="font-medium text-[var(--admin-accent-strong)] hover:underline"
        >
            {children}
        </button>
    );
}
