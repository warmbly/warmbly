// Mailbox detail — a themed right slide-over with five tabs:
//   Overview   read-only at-a-glance: health, today's usage, warmup status, identity
//   Analytics  warmup volume series + summary metrics
//   Warmup     editable warmup ramp config + live status
//   Sending    human sending behaviour: working days, hours, lunch, volume, spacing
//   Settings   profile, signature, tags, sending caps, tracking domain
// Edits across Warmup + Settings share one form; a sticky save bar commits
// them via PATCH /emails/:id. Sending owns its own save (PUT
// /emails/:id/behavior) because the profile is a separate resource with its own
// validation. Read data: /analytics/accounts/:id and /analytics/warmup?email_id=.

import React, { useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import AdvisorStrip from "@/components/app/advisor/AdvisorStrip";
import {
    XIcon,
    GaugeIcon,
    BarChart3Icon,
    FlameIcon,
    Settings2Icon,
    ClockFadingIcon,
    CheckCircle2Icon,
    AlertTriangleIcon,
    AlertCircleIcon,
    ClockIcon,
    CalendarIcon,
    ReplyIcon,
    SendIcon,
    CopyIcon,
    CheckIcon,
    PlayIcon,
    PauseIcon,
    ShieldCheckIcon,
    ShieldAlertIcon,
    BanIcon,
    HourglassIcon,
    XCircleIcon,
    RefreshCwIcon,
    type LucideIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type AccountStatusModel from "@/lib/api/models/app/analytics/AccountStatus";
import type { AccountError } from "@/lib/api/models/app/analytics/AccountStatus";
import useAccountStatus from "@/lib/api/hooks/app/analytics/useAccountStatus";
import useWarmupAnalytics from "@/lib/api/hooks/app/analytics/useWarmupAnalytics";
import useUpdateEmail from "@/lib/api/hooks/app/emails/useUpdateEmail";
import useWarmupLifecycle from "@/lib/api/hooks/app/emails/useWarmupLifecycle";
import useSendHold from "@/lib/api/hooks/app/emails/useSendHold";
import useWarmupBanStatus from "@/lib/api/hooks/app/emails/useWarmupBanStatus";
import useAppealWarmupBan from "@/lib/api/hooks/app/emails/useAppealWarmupBan";
import useAuthCheck from "@/lib/api/hooks/app/emails/useAuthCheck";
import useRefreshAuthCheck from "@/lib/api/hooks/app/emails/useRefreshAuthCheck";
import useUpdateEmailTrackingDomain from "@/lib/api/hooks/app/emails/useUpdateEmailTrackingDomain";
import useEmailTrackingDomain from "@/lib/api/hooks/app/emails/useEmailTrackingDomain";
import useVerifyEmailTrackingDomain from "@/lib/api/hooks/app/emails/useVerifyEmailTrackingDomain";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { useQueryClient } from "@tanstack/react-query";
import reauthEmailOAuth from "@/lib/api/client/app/emails/reauthEmailOAuth";
import onboardOAuthFinish from "@/lib/api/client/app/emails/onboardOAuthFinish";
import { openEmailOAuthPopup } from "@/lib/emails/emailOAuthPopup";
import UpdateCredentialsDialog from "./UpdateCredentialsDialog";
import EmailEditor from "../EmailEditor";
import SendingBehaviorTab from "./SendingBehaviorTab";
import SyncStatusCard from "./SyncStatusCard";
import CloudWarmupCard from "./CloudWarmupCard";
import useCloudPool from "@/hooks/useCloudPool";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import useSendingBehavior from "@/lib/api/hooks/app/emails/useSendingBehavior";
import useSendingPlan from "@/lib/api/hooks/app/emails/useSendingPlan";
import { minutesToClock, secondsToLabel } from "@/lib/api/models/app/emails/SendingBehavior";
import TagSelector from "../popup/select/TagSelector";
import TimeSelect from "@/components/ui/TimeSelect";
import { DitherBarChart } from "@/components/ui/dither";
import WeekdayBitmask from "../campaigns/schedule/WeekdayBitmask";
import { Loading } from "@/components/loader";
import { NumberInput, TextInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import { cn } from "@/lib/utils";

/* ── small themed primitives ─────────────────────── */

const Eyebrow = ({ children }: { children: React.ReactNode }) => (
    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</div>
);

// Warmup volume dropping, or a ramp that stops climbing, reads as a bug when
// nothing says why. The two states end at different times, so they are worded
// separately.
function RampHoldNotice({ hold }: { hold: import("@/lib/api/models/app/analytics/AccountStatus").WarmupRampHold }) {
    const hours = Math.max(0, Math.round((new Date(hold.resumes_at).getTime() - Date.now()) / 3_600_000));
    const resumesIn = hours > 0 ? ` for about ${hours} more ${hours === 1 ? "hour" : "hours"}` : "";
    return (
        <div className="px-5 py-4">
            <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2.5 flex items-start gap-2">
                <AlertTriangleIcon className="w-3.5 h-3.5 mt-px shrink-0 text-amber-600" />
                <div className="min-w-0">
                    <p className="text-[12.5px] font-medium text-amber-900">
                        {hold.volume_cut ? "Warmup volume held back" : "Warmup ramp paused"}
                    </p>
                    <p className="text-[11.5px] text-amber-800/90 leading-relaxed mt-0.5">
                        {hold.volume_cut ? (
                            <>
                                {hold.placements === 1 ? "1 warmup email" : `${hold.placements} warmup emails`} landed in spam in
                                the last 48 hours{hold.sends > 0 ? ` out of ${hold.sends} sent` : ""}. Today's target is cut by a
                                quarter and the daily increase is paused{resumesIn}.
                            </>
                        ) : (
                            <>
                                Volume is back to normal, but the daily increase stays paused{resumesIn} after a recent spam
                                placement.
                            </>
                        )}{" "}
                        It resumes on its own if nothing else lands in spam.
                    </p>
                </div>
            </div>
        </div>
    );
}

// A mailbox that has quietly stopped receiving campaign sends looks broken.
// hasHealthSignal is warmup_health presence: the pool row the rebalancer reads, not the warmup setting.
function LifecycleNotice({
    mailboxId,
    state,
    hasHealthSignal,
}: {
    mailboxId: string;
    state: import("@/lib/api/models/app/analytics/AccountStatus").SendLifecycleState;
    hasHealthSignal: boolean;
}) {
    const hold = useSendHold(mailboxId);
    const reserve = state.state === "reserve";
    const copy = reserve
        ? "You are holding this mailbox out of campaigns. Warmup keeps running. Turn the hold off below to put it back into rotation."
        : hasHealthSignal
            ? "This mailbox is resting: campaigns are not sending from it while its warmup health recovers. It returns on its own once that health is back and has held steady for three days."
            : "This mailbox is resting, but it is in no warmup pool, so there is no health signal to recover on. It returns on its own after three days of rest, or now if you put it back.";
    const resume = () =>
        hold.mutate(false, {
            onSuccess: (data) =>
                toast.success(
                    data.state === "resting"
                        ? "Still resting: its warmup health is throttled or worse, so it stays out until that recovers"
                        : "Mailbox back in campaign rotation",
                ),
            onError: (e) => toast.error(buildError(e as unknown as AppError)),
        });
    return (
        <div className="px-5 py-4">
            <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2.5 flex items-start gap-2">
                <PauseIcon className="w-3.5 h-3.5 mt-px shrink-0 text-slate-500" />
                <div className="min-w-0 flex-1">
                    <p className="text-[12.5px] font-medium text-slate-900">
                        Not sending campaigns ({reserve ? "held" : state.state})
                    </p>
                    <p className="text-[11.5px] text-slate-600 leading-relaxed mt-0.5">
                        {copy}
                        {!reserve && state.reason ? ` ${state.reason}.` : ""}
                    </p>
                    {!reserve && (
                        <button
                            type="button"
                            onClick={resume}
                            disabled={hold.isPending}
                            className="mt-2 h-7 px-2.5 inline-flex items-center rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:border-sky-400 hover:text-sky-700 disabled:opacity-50 transition-colors"
                        >
                            {hold.isPending ? "Putting back…" : "Put back into campaigns"}
                        </button>
                    )}
                </div>
            </div>
        </div>
    );
}

// The owner's switch for the reserve lifecycle state.
function SendHoldControl({ mailboxId, state }: { mailboxId: string; state?: import("@/lib/api/models/app/analytics/AccountStatus").SendLifecycleState }) {
    const hold = useSendHold(mailboxId);
    const held = state?.state === "reserve";
    const toggle = (v: boolean) =>
        hold.mutate(v, {
            onSuccess: (data) =>
                toast.success(
                    v
                        ? "Mailbox held out of campaigns"
                        : data.state === "resting"
                            ? "Hold released. The mailbox rests until its warmup health recovers"
                            : "Mailbox back in campaign rotation",
                ),
            onError: (e) => toast.error(buildError(e as unknown as AppError)),
        });
    return (
        <div className="px-5 py-4 flex items-start justify-between gap-3">
            <div className="min-w-0">
                <div className="text-[12.5px] font-medium text-slate-900">Hold from campaigns</div>
                <div className="text-[11px] text-slate-400">
                    Keeps this mailbox out of campaign sending until you turn the hold off. Warmup is not affected, and Warmbly never releases a hold on its own.
                </div>
            </div>
            <div className="shrink-0">
                <Toggle value={held} onChange={toggle} disabled={hold.isPending} />
            </div>
        </div>
    );
}

// A cold cap below the configured one reads as a bug unless it says why.
function ColdRampNotice({ ramp }: { ramp: import("@/lib/api/models/app/analytics/AccountStatus").ColdRampInfo }) {
    return (
        <div className="px-5 py-4">
            <div className="rounded-md border border-sky-200 bg-sky-50 px-3 py-2.5 flex items-start gap-2">
                <GaugeIcon className="w-3.5 h-3.5 mt-px shrink-0 text-sky-600" />
                <div className="min-w-0">
                    <p className="text-[12.5px] font-medium text-sky-900">
                        Easing into cold sending: {ramp.ceiling} of {ramp.mailbox_cap} a day
                    </p>
                    <p className="text-[11.5px] text-sky-800/90 leading-relaxed mt-0.5">
                        {ramp.held ? (
                            <>
                                The climb is paused after a recent spam placement. It resumes on its own, then adds 5 a
                                day until it reaches {ramp.mailbox_cap}.
                            </>
                        ) : (
                            <>
                                Going straight from warmup to a full cold cap is the volume jump mailbox providers
                                penalise, so this adds 5 a day instead. At this rate it reaches {ramp.mailbox_cap} in
                                about {ramp.days_to_full_cap} {ramp.days_to_full_cap === 1 ? "day" : "days"}.
                            </>
                        )}
                    </p>
                </div>
            </div>
        </div>
    );
}

function StatCard({ label, value, sub, accent }: { label: string; value: React.ReactNode; sub?: string; accent?: boolean }) {
    return (
        <div className="px-4 py-3.5">
            <Eyebrow>{label}</Eyebrow>
            <div className={cn("mt-1 text-[24px] font-light leading-none tabular-nums", accent ? "text-sky-600" : "text-slate-900")}>{value}</div>
            {sub && <div className="mt-1.5 text-[10.5px] text-slate-400 font-mono truncate">{sub}</div>}
        </div>
    );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="flex items-center justify-between gap-3 px-5 h-10 border-b border-slate-200/60 text-[12.5px]">
            <span className="text-slate-500">{label}</span>
            <span className="text-slate-900 truncate text-right">{children}</span>
        </div>
    );
}

function FieldShell({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
    return (
        <div>
            <label className="block text-[12px] font-medium text-slate-700 mb-1">{label}</label>
            {children}
            {hint && <p className="text-[10.5px] text-slate-400 mt-1 leading-relaxed">{hint}</p>}
        </div>
    );
}

function NumField({ value, onChange, suffix, max }: { value: number; onChange: (v: number) => void; suffix?: string; max?: number }) {
    // Themed number field with our own steppers, no native spinner.
    return (
        <NumberInput
            value={value}
            onChange={onChange}
            suffix={suffix}
            min={0}
            max={max}
            align="right"
            className="w-full h-9"
        />
    );
}

// min_wait_time is stored in SECONDS. Render it as a duration so the number
// can never be read as minutes — a 600 shown as "600 min" (or a 10 typed into a
// field labelled minutes) is the difference between a 10-minute gap and a
// 10-second burst.
function formatGap(seconds: number): string {
    if (!Number.isFinite(seconds) || seconds < 60) return `${seconds ?? 0}s`;
    const m = Math.floor(seconds / 60);
    const s = seconds % 60;
    return s === 0 ? `${m} min` : `${m} min ${s}s`;
}

function statusTone(status: string) {
    const s = status?.toLowerCase();
    if (s === "active" || s === "healthy") return "bg-emerald-50 text-emerald-700 border-emerald-100";
    if (s === "warming" || s === "warning") return "bg-amber-50 text-amber-700 border-amber-100";
    if (s === "revoked" || s === "error" || s === "inactive") return "bg-rose-50 text-rose-700 border-rose-100";
    return "bg-slate-100 text-slate-600 border-slate-200";
}

/* ── tabs ─────────────────────── */

const TABS: { key: string; label: string; icon: LucideIcon }[] = [
    { key: "overview", label: "Overview", icon: GaugeIcon },
    { key: "analytics", label: "Analytics", icon: BarChart3Icon },
    { key: "warmup", label: "Warmup", icon: FlameIcon },
    { key: "sending", label: "Sending", icon: ClockFadingIcon },
    { key: "settings", label: "Settings", icon: Settings2Icon },
];

/* ═══════════════════════════════════════════
   Drawer shell
   ═══════════════════════════════════════════ */

export default function InboxDetails({
    emails,
    view,
    setView,
    initialTab = "overview",
    canWarmup = true,
}: {
    emails: Inbox[] | null;
    view: string;
    setView: React.Dispatch<React.SetStateAction<string>>;
    initialTab?: string;
    canWarmup?: boolean;
}) {
    const mailbox = emails?.find((e) => e.id === view) ?? null;
    const close = () => setView("");

    return (
        <AnimatePresence>
            {view && mailbox && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="fixed inset-0 z-40 bg-slate-900/40"
                        onClick={close}
                    />
                    <motion.aside
                        initial={{ x: "100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "100%" }}
                        transition={{ type: "spring", damping: 32, stiffness: 320 }}
                        className="fixed right-0 top-0 z-50 h-full w-full sm:w-[600px] bg-white border-l border-slate-200 shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)] flex flex-col"
                    >
                        <Detail key={mailbox.id} mailbox={mailbox} onClose={close} initialTab={initialTab} canWarmup={canWarmup} />
                    </motion.aside>
                </>
            )}
        </AnimatePresence>
    );
}

/* ── editable fields tracked for the save bar ─────────────────────── */
const EDITABLE: (keyof Inbox)[] = [
    "name", "signature_html", "signature_plain", "signature_sync", "signature_code",
    "tags", "campaign_limit", "min_wait_time", "reply_to", "save_to_sent",
    "warmup_base", "warmup_max", "warmup_increase", "warmup_reply_rate",
    "warmup_tag", "warmup_start_time", "warmup_end_time", "warmup_days",
];

function Detail({ mailbox, onClose, initialTab = "overview", canWarmup = true }: { mailbox: Inbox; onClose: () => void; initialTab?: string; canWarmup?: boolean }) {
    const [tab, setTab] = useState(initialTab);
    const [form, setForm] = useState<Inbox>(mailbox);
    const update = (patch: Partial<Inbox>) => setForm((f) => ({ ...f, ...patch }));

    usePresenceResource(mailbox.id ? `mailbox:${mailbox.id}` : null, "editing");

    const status = useAccountStatus(mailbox.id);
    const { from, to } = useMemo(() => {
        const end = new Date();
        const start = new Date();
        start.setDate(start.getDate() - 29);
        const fmt = (d: Date) => d.toISOString().slice(0, 10);
        return { from: fmt(start), to: fmt(end) };
    }, []);
    const warmup = useWarmupAnalytics(mailbox.id, from, to);
    const mutation = useUpdateEmail(mailbox.id);

    const dirty = useMemo(
        () => EDITABLE.some((k) => JSON.stringify(form[k]) !== JSON.stringify(mailbox[k])),
        [form, mailbox],
    );

    const save = async () => {
        const patch: Partial<Inbox> = {};
        for (const k of EDITABLE) {
            if (JSON.stringify(form[k]) !== JSON.stringify(mailbox[k])) {
                (patch as Record<string, unknown>)[k] = form[k];
            }
        }
        try {
            await mutation.mutateAsync(patch);
            toast.success("Mailbox updated");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const initials = mailbox.email.slice(0, 2).toUpperCase();

    return (
        <>
            {/* Header */}
            <div className="shrink-0 px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                <div className="w-8 h-8 rounded-lg bg-sky-50 text-sky-700 flex items-center justify-center text-[11px] font-semibold shrink-0">
                    {initials}
                </div>
                <div className="min-w-0 flex-1">
                    <div className="text-[13px] font-medium text-slate-900 truncate">{mailbox.email}</div>
                    <div className="text-[10.5px] text-slate-400 capitalize">{mailbox.provider?.replace("_", "/")}</div>
                </div>
                <span className={cn("h-5 px-2 rounded-full border text-[10px] font-semibold uppercase tracking-wide inline-flex items-center shrink-0", statusTone(mailbox.status))}>
                    {mailbox.status}
                </span>
                <ResourceViewers resource={mailbox.id ? `mailbox:${mailbox.id}` : null} className="shrink-0" />
                <button onClick={onClose} aria-label="Close" className="w-7 h-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0">
                    <XIcon className="w-4 h-4" />
                </button>
            </div>

            {/* Tabs */}
            <div className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200 overflow-x-auto">
                {TABS.map((t) => {
                    const active = tab === t.key;
                    return (
                        <button
                            key={t.key}
                            onClick={() => setTab(t.key)}
                            className={cn(
                                "relative h-10 px-2.5 inline-flex shrink-0 items-center gap-1.5 text-[12.5px] transition-colors",
                                active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                            )}
                        >
                            <t.icon className="w-3.5 h-3.5" />
                            {t.label}
                            {active && (
                                <motion.span
                                    layoutId="inbox-tab-underline"
                                    className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                                    transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                />
                            )}
                        </button>
                    );
                })}
            </div>

            {/* Body */}
            <div className="flex-1 min-h-0 overflow-y-auto">
                {tab === "overview" && <OverviewTab status={status.data} loading={status.isPending} mailbox={mailbox} />}
                {tab === "analytics" && <AnalyticsTab warmup={warmup.data} loading={warmup.isPending} />}
                {tab === "warmup" && <WarmupTab form={form} update={update} status={status.data} mailbox={mailbox} canWarmup={canWarmup} />}
                {tab === "sending" && <SendingBehaviorTab mailboxId={mailbox.id} />}
                {tab === "settings" && <SettingsTab form={form} update={update} mailbox={mailbox} />}
            </div>

            {/* Save bar — only when something changed */}
            <AnimatePresence>
                {dirty && (
                    <motion.div
                        initial={{ y: 60 }}
                        animate={{ y: 0 }}
                        exit={{ y: 60 }}
                        transition={{ duration: 0.2 }}
                        className="shrink-0 h-14 px-5 flex items-center gap-2 border-t border-slate-200 bg-slate-50/60"
                    >
                        <span className="text-[11.5px] text-slate-500">Unsaved changes</span>
                        <div className="ml-auto flex items-center gap-2">
                            <button onClick={() => setForm(mailbox)} className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors">
                                Discard
                            </button>
                            <button onClick={save} disabled={mutation.isPending} className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60">
                                {mutation.isPending && <Loading className="!w-3.5 h-3.5 text-white" />}
                                Save changes
                            </button>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </>
    );
}

/* ── Overview ─────────────────────── */

// Credential-class error codes a reconnect fixes (mirror of the backend's
// errx.CredentialMailErrorCodes). Any of these gets the reconnect button.
const CREDENTIAL_ERROR_CODES = new Set([
    "GOOGLE_AUTHENTICATION_FAILED",
    "AUTHENTICATION_FAILED",
    "AUTHORIZATION_FAILED",
    "INVALID_CREDENTIALS",
]);

// The missing re-verify button of issue #274. OAuth mailboxes re-run the
// provider consent in a popup; SMTP/IMAP mailboxes get a replacement-
// credentials dialog. Either way the backend renews the stored credential,
// clears the error, and reactivates the mailbox.
function ReconnectAction({ mailbox }: { mailbox: Inbox }) {
    const qc = useQueryClient();
    const [busy, setBusy] = useState(false);
    const [credsOpen, setCredsOpen] = useState(false);
    const oauth = mailbox.provider === "gmail" || mailbox.provider === "outlook";
    const providerLabel = mailbox.provider === "gmail" ? "Google" : "Microsoft";

    const reauth = async () => {
        if (busy) return;
        setBusy(true);
        try {
            const { url, state } = await reauthEmailOAuth(mailbox.id);
            const { code } = await openEmailOAuthPopup(url, state);
            await onboardOAuthFinish(code, state);
            toast.success("Mailbox re-authorized. It's back online.");
            qc.invalidateQueries({ queryKey: ["emails", "list"] });
            qc.invalidateQueries({ queryKey: ["analytics", "accounts"] });
        } catch (e) {
            toast.error(e instanceof Error ? e.message : buildError(e as AppError));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="mt-2">
            {oauth ? (
                <button
                    type="button"
                    onClick={() => void reauth()}
                    disabled={busy}
                    className="h-7 px-2.5 rounded-md bg-rose-600 hover:bg-rose-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {busy ? <Loading className="!w-3 h-3 text-white" /> : <ShieldCheckIcon className="w-3 h-3" />}
                    {busy ? "Waiting for authorization…" : `Re-authorize with ${providerLabel}`}
                </button>
            ) : (
                <button
                    type="button"
                    onClick={() => setCredsOpen(true)}
                    className="h-7 px-2.5 rounded-md bg-rose-600 hover:bg-rose-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    <ShieldCheckIcon className="w-3 h-3" />
                    Update credentials
                </button>
            )}
            <UpdateCredentialsDialog
                mailboxId={mailbox.id}
                mailboxEmail={mailbox.email}
                open={credsOpen}
                onClose={() => setCredsOpen(false)}
            />
        </div>
    );
}

function OverviewTab({ status, loading, mailbox }: { status?: import("@/lib/api/models/app/analytics/AccountStatus").default; loading: boolean; mailbox: Inbox }) {
    const health = status?.health;
    const usage = status?.daily_usage;
    const ws = status?.warmup_status;

    // When a sending-behaviour profile is on, TODAY'S rolled plan is the real
    // ceiling and spacing — not the mailbox's fixed cap and gap. Showing the
    // fixed numbers here would contradict what the mailbox actually does.
    const behavior = useSendingBehavior(mailbox.id);
    const personaOn = behavior.data?.enabled === true;
    const plan = useSendingPlan(mailbox.id, personaOn);
    const today = personaOn ? plan.data : undefined;

    const healthTone =
        health?.status === "healthy" ? { bar: "bg-emerald-500", text: "text-emerald-600", icon: CheckCircle2Icon }
            : health?.status === "warning" ? { bar: "bg-amber-500", text: "text-amber-600", icon: AlertTriangleIcon }
                : { bar: "bg-rose-500", text: "text-rose-600", icon: AlertCircleIcon };
    const HealthIcon = healthTone.icon;

    // One reconnect button per drawer, on the first credential-class error;
    // every such error is fixed by the same reconnect.
    const firstCredentialErrorId = status?.errors?.find((e) => CREDENTIAL_ERROR_CODES.has(e.error_code))?.id;

    const synced = mailbox.last_synced_at ? new Date(mailbox.last_synced_at) : null;

    return (
        <div className="divide-y divide-slate-200/60">
            {/* Whatever the Advisor has on this mailbox, above the numbers that
                produced it. This is where a row flag and a deep link both land. */}
            <AdvisorStrip
                entityType="email_account"
                entityId={mailbox.id}
                title=""
                limit={4}
                compact
                className="px-5 py-4"
            />

            {/* Health */}
            <div className="px-5 py-4">
                <div className="flex items-center justify-between">
                    <Eyebrow>Health</Eyebrow>
                    {health && (
                        <span className={cn("inline-flex items-center gap-1 text-[11px] font-medium capitalize", healthTone.text)}>
                            <HealthIcon className="w-3.5 h-3.5" /> {health.status}
                        </span>
                    )}
                </div>
                <div className="mt-2 flex items-end gap-2">
                    <span className="text-[30px] font-light leading-none text-slate-900 tabular-nums">{loading ? "—" : (health?.score ?? "—")}</span>
                    <span className="text-[12px] text-slate-400 mb-1">/ 100</span>
                </div>
                <div className="mt-2.5 h-1.5 w-full rounded-full bg-slate-100 overflow-hidden">
                    <div className={cn("h-full rounded-full transition-all", healthTone.bar)} style={{ width: `${health?.score ?? 0}%` }} />
                </div>
                {health?.issues && health.issues.length > 0 && (
                    <ul className="mt-3 space-y-1">
                        {health.issues.map((i, k) => (
                            <li key={k} className="text-[11.5px] text-slate-500 flex items-start gap-1.5">
                                <span className="text-amber-500 mt-0.5">•</span> {i}
                            </li>
                        ))}
                    </ul>
                )}
            </div>

            {/* Sync: import progress and fair-use status */}
            <SyncStatusCard mailboxId={mailbox.id} />

            {/* Key stats */}
            <div className="grid grid-cols-2 divide-x divide-y divide-slate-200/60">
                <StatCard
                    label="Sent today"
                    value={usage ? usage.campaign_sent : "—"}
                    sub={
                        today
                            ? today.is_working_day
                                ? `of ${today.daily_limit} rolled for today`
                                : "not a sending day"
                            : usage
                                ? `of ${usage.campaign_limit}/day cap`
                                : undefined
                    }
                />
                <StatCard label="Warmup today" value={ws ? ws.current_volume : (usage?.warmup_sent ?? "—")} sub={ws ? `target ${ws.target_volume}` : undefined} accent />
                <StatCard label="Reply rate" value={ws ? `${ws.reply_rate}%` : "—"} sub="warmup replies" />
                <StatCard label="Days warming" value={ws ? ws.days_active : "—"} sub={ws ? `max ${ws.max_volume}/day` : undefined} />
            </div>

            {ws?.ramp_hold && <RampHoldNotice hold={ws.ramp_hold} />}
            {status?.send_lifecycle && <LifecycleNotice mailboxId={mailbox.id} state={status.send_lifecycle} hasHealthSignal={!!status.warmup_health} />}
            {status?.cold_ramp && <ColdRampNotice ramp={status.cold_ramp} />}
            {status && <SendHoldControl mailboxId={mailbox.id} state={status.send_lifecycle} />}

            {/* Errors */}
            {status?.errors && status.errors.length > 0 && (
                <div className="px-5 py-4">
                    <Eyebrow>Needs attention</Eyebrow>
                    <div className="mt-2 space-y-2">
                        {status.errors.map((e: AccountError) => (
                            <div key={e.id} className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2">
                                <div className="text-[12px] font-medium text-rose-800">{e.title}</div>
                                <div className="text-[11px] text-rose-700/90 mt-0.5 leading-relaxed">{e.message}</div>
                                {e.action_required && <div className="text-[11px] text-rose-900 mt-1 font-medium">{e.action_required}</div>}
                                {e.id === firstCredentialErrorId && <ReconnectAction mailbox={mailbox} />}
                            </div>
                        ))}
                    </div>
                </div>
            )}

            {/* Identity */}
            <div>
                <Row label="Provider"><span className="capitalize">{mailbox.provider?.replace("_", "/")}</span></Row>
                <Row label="Tracking domain">{mailbox.tracking_domain || <span className="text-slate-400">Not set</span>}</Row>
                <Row label="Daily cap">
                    {today?.is_working_day
                        ? `${today.daily_limit} / day today (${behavior.data?.daily_limit_min}-${behavior.data?.daily_limit_max} range)`
                        : `${mailbox.campaign_limit} / day`}
                </Row>
                <Row label="Min gap">
                    {today
                        ? `${secondsToLabel(today.gap_min_seconds)}-${secondsToLabel(today.gap_max_seconds)}`
                        : formatGap(mailbox.min_wait_time)}
                </Row>
                {today?.is_working_day && (
                    <Row label="Workday">
                        {minutesToClock(today.work_start_minute)}-{minutesToClock(today.work_end_minute)}
                        {today.lunch_start_minute !== null && today.lunch_end_minute !== null && (
                            <span className="text-slate-400">
                                {" "}(lunch {minutesToClock(today.lunch_start_minute)}-{minutesToClock(today.lunch_end_minute)})
                            </span>
                        )}
                    </Row>
                )}
                <Row label="Last synced">
                    <span className="inline-flex items-center gap-1.5 text-slate-600">
                        <ClockIcon className="w-3.5 h-3.5 text-slate-400" />
                        {synced ? synced.toLocaleString() : "Never"}
                    </span>
                </Row>
                <Row label="Connected">
                    <span className="inline-flex items-center gap-1.5 text-slate-600">
                        <CalendarIcon className="w-3.5 h-3.5 text-slate-400" />
                        {new Date(mailbox.created_at).toLocaleDateString()}
                    </span>
                </Row>
            </div>
        </div>
    );
}

/* ── Analytics ─────────────────────── */

function AnalyticsTab({ warmup, loading }: { warmup?: import("@/lib/api/models/app/analytics/WarmupAnalytics").default; loading: boolean }) {
    // Tap affordance for touch devices, where the native title tooltip never fires.
    const [selectedDay, setSelectedDay] = useState<string | null>(null);
    if (loading) {
        return <div className="py-20 flex items-center justify-center"><Loading className="w-5 h-5 text-sky-500" /></div>;
    }
    if (!warmup || warmup.daily_stats.length === 0) {
        return (
            <div className="px-5 py-16 text-center">
                <BarChart3Icon className="w-5 h-5 text-slate-300 mx-auto mb-2" />
                <p className="text-[12.5px] text-slate-700 font-medium">No warmup activity yet</p>
                <p className="text-[11.5px] text-slate-400 mt-1 max-w-[34ch] mx-auto leading-relaxed">Once this mailbox starts warming, its daily volume and replies will chart here.</p>
            </div>
        );
    }
    const s = warmup.summary;
    const chartData = warmup.daily_stats.map((d) => ({
        key: d.date,
        value: d.emails_sent,
        hint: `${d.date}: ${d.emails_sent} sent / ${d.target_volume} target · ${d.emails_replied} replies`,
    }));
    const targets = warmup.daily_stats.map((d) => d.target_volume);
    const selectedIndex = selectedDay ? warmup.daily_stats.findIndex((d) => d.date === selectedDay) : -1;

    return (
        <div className="divide-y divide-slate-200/60">
            <div className="grid grid-cols-2 divide-x divide-y divide-slate-200/60">
                <StatCard label="Total sent" value={s.total_sent} sub={`${s.average_daily.toFixed(1)}/day avg`} />
                <StatCard label="Replies" value={s.total_replied} sub={`${s.reply_rate.toFixed(1)}% reply rate`} accent />
                <StatCard label="Target progress" value={`${Math.round(s.target_progress)}%`} sub="toward max volume" />
                <StatCard label="Days active" value={s.days_active} sub={`${warmup.date_range.from} → ${warmup.date_range.to}`} />
            </div>

            <div className="px-5 py-4">
                <div className="flex items-center justify-between mb-3">
                    <Eyebrow>Daily volume</Eyebrow>
                    <div className="flex items-center gap-3 text-[10px] text-slate-400">
                        <span className="inline-flex items-center gap-1"><span className="w-2 h-2 rounded-sm bg-sky-500" /> Sent</span>
                        <span className="inline-flex items-center gap-1"><span className="w-2 h-2 rounded-sm bg-slate-200" /> Target</span>
                    </div>
                </div>
                <DitherBarChart
                    data={chartData}
                    ghost={targets}
                    height={112}
                    selected={selectedIndex >= 0 ? selectedIndex : null}
                    onSelect={(i) =>
                        setSelectedDay((cur) => {
                            const date = warmup.daily_stats[i]?.date ?? null;
                            return cur === date ? null : date;
                        })
                    }
                />
                {selectedDay && (() => {
                    const d = warmup.daily_stats.find((s) => s.date === selectedDay);
                    if (!d) return null;
                    return (
                        <p className="mt-2 text-[11px] text-slate-500 font-mono tabular-nums">
                            {d.date}: {d.emails_sent} sent / {d.target_volume} target · {d.emails_replied} replies
                        </p>
                    );
                })()}
            </div>
        </div>
    );
}

/* ── Warmup (editable) ─────────────────────── */

const WEEKDAYS = ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"];

const warmupStateTone: Record<string, { text: string; bar: string; label: string }> = {
    healthy: { text: "text-emerald-600", bar: "bg-emerald-500", label: "Healthy" },
    watch: { text: "text-amber-600", bar: "bg-amber-500", label: "Watch" },
    throttled: { text: "text-amber-700", bar: "bg-amber-500", label: "Throttled" },
    quarantined: { text: "text-rose-600", bar: "bg-rose-500", label: "Quarantined" },
    blocked: { text: "text-rose-700", bar: "bg-rose-600", label: "Blocked" },
};

/* ── Warmup ban banner + appeal form ─────────────────────── */

// Shown at the top of the Warmup tab when a mailbox has been blocked from the
// warmup pool (e.g. for deleting or spam-marking warmup mail). Explains why,
// lets the owner submit an appeal, and flips to "under review" once one is
// pending. Renders nothing while loading or when the mailbox isn't blocked.
function WarmupBanBanner({ emailId }: { emailId: string }) {
    const ban = useWarmupBanStatus(emailId);
    const appeal = useAppealWarmupBan(emailId);
    const [open, setOpen] = useState(false);
    const [reason, setReason] = useState("");

    const data = ban.data;
    if (!data || !data.blocked) return null;

    const submit = async () => {
        const trimmed = reason.trim();
        if (!trimmed) {
            toast.error("Please describe why this mailbox should be reinstated.");
            return;
        }
        try {
            await appeal.mutateAsync(trimmed);
            toast.success("Appeal submitted — our team will review it.");
            setOpen(false);
            setReason("");
        } catch (e) {
            toast.error(buildError(e as unknown as AppError));
        }
    };

    return (
        <div className="px-5 py-4">
            <div className="rounded-md border border-rose-200 bg-rose-50 p-3.5">
                <div className="flex items-start gap-2.5">
                    <div className="w-7 h-7 rounded-md bg-rose-100 text-rose-600 flex items-center justify-center shrink-0">
                        <BanIcon className="w-4 h-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                            <span className="text-[12.5px] font-semibold text-rose-900">Blocked from warmup</span>
                            <span className="h-5 px-2 rounded-full border border-rose-200 bg-white/60 text-rose-700 text-[10px] font-semibold uppercase tracking-wide inline-flex items-center capitalize">
                                {data.health_state}
                            </span>
                        </div>
                        <p className="mt-1 text-[11.5px] text-rose-800/90 leading-relaxed">
                            {data.reason || "This mailbox was removed from the warmup pool to protect shared sender reputation."}
                        </p>
                        {(data.blocked_at || data.blocked_until) && (
                            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-[10.5px] text-rose-700/80">
                                {data.blocked_at && (
                                    <span className="inline-flex items-center gap-1">
                                        <ClockIcon className="w-3 h-3" /> Blocked {new Date(data.blocked_at).toLocaleDateString()}
                                    </span>
                                )}
                                {data.blocked_until && (
                                    <span className="inline-flex items-center gap-1">
                                        <CalendarIcon className="w-3 h-3" /> Until {new Date(data.blocked_until).toLocaleDateString()}
                                    </span>
                                )}
                            </div>
                        )}

                        {/* Appeal affordance */}
                        {data.pending_appeal ? (
                            <div className="mt-3 inline-flex items-center gap-1.5 rounded-md border border-amber-200 bg-amber-50 px-2.5 py-1.5 text-[11.5px] font-medium text-amber-800">
                                <HourglassIcon className="w-3.5 h-3.5" /> Appeal submitted — under review
                            </div>
                        ) : data.can_appeal ? (
                            open ? (
                                <div className="mt-3 space-y-2">
                                    <label className="block text-[10px] uppercase tracking-[0.14em] text-rose-700/80 font-medium">
                                        Appeal reason
                                    </label>
                                    <textarea
                                        value={reason}
                                        onChange={(e) => setReason(e.target.value)}
                                        rows={3}
                                        autoFocus
                                        placeholder="Tell us what changed or why this block is a mistake…"
                                        className="w-full px-2.5 py-2 rounded-md border border-rose-200 bg-white text-[12px] text-slate-900 placeholder:text-slate-400 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 resize-none transition-colors"
                                    />
                                    <div className="flex items-center gap-2">
                                        <button
                                            onClick={submit}
                                            disabled={appeal.isPending || !reason.trim()}
                                            className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                        >
                                            {appeal.isPending && <Loading className="!w-3.5 h-3.5 text-white" />}
                                            Submit appeal
                                        </button>
                                        <button
                                            onClick={() => { setOpen(false); setReason(""); }}
                                            disabled={appeal.isPending}
                                            className="h-8 px-3 rounded-md border border-rose-200 hover:border-rose-300 text-[12px] text-rose-700 hover:text-rose-900 transition-colors disabled:opacity-60"
                                        >
                                            Cancel
                                        </button>
                                    </div>
                                </div>
                            ) : (
                                <button
                                    onClick={() => setOpen(true)}
                                    className="mt-3 h-8 px-3 rounded-md bg-rose-600 hover:bg-rose-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                                >
                                    <ShieldAlertIcon className="w-3.5 h-3.5" /> Appeal this block
                                </button>
                            )
                        ) : null}
                    </div>
                </div>
            </div>
        </div>
    );
}

/* ── Domain authentication (SPF / DKIM / DMARC live check) ─────────────────────── */

// One row per auth record. Green check when present/aligned, red cross when
// missing. Optional detail (selectors, the SPF record, the DMARC policy) is
// shown muted underneath.
function AuthRecordRow({ label, ok, detail }: { label: string; ok: boolean; detail?: React.ReactNode }) {
    return (
        <div className="flex items-start justify-between gap-3 py-2 border-b border-slate-200/60 last:border-b-0">
            <div className="min-w-0">
                <div className="text-[12.5px] font-medium text-slate-900">{label}</div>
                {detail && <div className="mt-0.5 text-[10.5px] text-slate-500 font-mono break-all leading-relaxed">{detail}</div>}
            </div>
            <span className={cn("inline-flex items-center gap-1 shrink-0 text-[11px] font-medium", ok ? "text-emerald-600" : "text-rose-600")}>
                {ok ? <CheckCircle2Icon className="w-3.5 h-3.5" /> : <XCircleIcon className="w-3.5 h-3.5" />}
                {ok ? "Found" : "Missing"}
            </span>
        </div>
    );
}

// The banner above the records, shown only when the stored state is "failing".
// The gate is invisible otherwise, and an owner whose campaigns have stopped
// needs to be told that here rather than inferring it from a paused campaign.
function AuthGateNotice({ mailbox }: { mailbox: Inbox }) {
    if (mailbox.auth_state !== "failing") return null;

    const since = mailbox.auth_failing_since ? new Date(mailbox.auth_failing_since) : null;
    return (
        <div className="mt-3 rounded-md border border-rose-200 bg-rose-50 px-3 py-2.5 flex gap-2.5">
            <ShieldAlertIcon className="w-4 h-4 text-rose-600 shrink-0 mt-0.5" />
            <div className="min-w-0 text-[11.5px] text-rose-900/90 leading-relaxed">
                <span className="font-medium">This domain is failing authentication.</span>{" "}
                Cold sending and warmup from this mailbox stop while it stays that way
                {since ? `, failing since ${since.toLocaleDateString()}` : ""}. Add the missing DNS
                records at your registrar, then re-check below to clear it straight away.
            </div>
        </div>
    );
}

function AuthCheckPanel({ mailbox }: { mailbox: Inbox }) {
    const emailId = mailbox.id;
    const [open, setOpen] = useState(false);
    const check = useAuthCheck(emailId, open);
    // Re-checking RECORDS the verdict, which is what lifts the send gate, so
    // the button is a write and not a query refetch.
    const refresh = useRefreshAuthCheck(emailId);
    const data = refresh.data ?? check.data;
    const busy = check.isFetching || refresh.isPending;

    return (
        <div className="px-5 py-4">
            <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                    <Eyebrow>Domain authentication</Eyebrow>
                    <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">Live SPF, DKIM &amp; DMARC check on the sending domain.</p>
                </div>
                <button
                    onClick={() => {
                        setOpen(true);
                        if (open) {
                            refresh.mutate(undefined, {
                                onError: (e) => toast.error(buildError(e as unknown as AppError)),
                            });
                        }
                    }}
                    disabled={busy}
                    className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60 shrink-0"
                >
                    {busy ? <Loading className="!w-3.5 h-3.5" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                    {open ? "Re-check" : "Check"}
                </button>
            </div>

            <AuthGateNotice mailbox={mailbox} />

            {open && (
                <div className="mt-3">
                    {check.isError ? (
                        <div className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2.5 text-[11.5px] text-rose-700 leading-relaxed">
                            {buildError(check.error as unknown as AppError)}
                        </div>
                    ) : check.isFetching && !data ? (
                        <div className="rounded-md border border-slate-200 bg-slate-50/70 px-3 py-3 flex items-center gap-2 text-[12px] text-slate-500">
                            <Loading className="!w-3.5 h-3.5" /> Looking up DNS records…
                        </div>
                    ) : data ? (
                        <div className="rounded-md border border-slate-200 bg-white">
                            <div className={cn("flex items-start gap-2 px-3 py-2.5 border-b border-slate-200/60", data.all_aligned ? "text-emerald-700" : "text-amber-700")}>
                                {data.all_aligned ? <ShieldCheckIcon className="w-4 h-4 shrink-0 mt-0.5" /> : <ShieldAlertIcon className="w-4 h-4 shrink-0 mt-0.5" />}
                                <div className="min-w-0">
                                    <div className="text-[12px] font-medium">{data.all_aligned ? "Authentication aligned" : "Authentication needs attention"}</div>
                                    {data.summary && <div className="mt-0.5 text-[11px] text-slate-500 leading-relaxed">{data.summary}</div>}
                                </div>
                            </div>
                            <div className="px-3">
                                <AuthRecordRow label="SPF" ok={data.spf_found} detail={data.spf_record} />
                                <AuthRecordRow
                                    label="DKIM"
                                    ok={data.dkim_found}
                                    detail={data.dkim_selectors && data.dkim_selectors.length > 0 ? `selectors: ${data.dkim_selectors.join(", ")}` : undefined}
                                />
                                <AuthRecordRow
                                    label="DMARC"
                                    ok={data.dmarc_found}
                                    detail={
                                        data.dmarc_found && data.dmarc_policy
                                            ? data.dmarc_inherited
                                                ? `policy: ${data.dmarc_policy} (inherited from ${data.dmarc_domain})`
                                                : `policy: ${data.dmarc_policy}`
                                            : undefined
                                    }
                                />
                            </div>
                            <div className="px-3 py-2 text-[10.5px] text-slate-400 font-mono truncate border-t border-slate-200/60">{data.domain}</div>
                        </div>
                    ) : null}
                </div>
            )}
        </div>
    );
}

function WarmupTab({ form, update, status, mailbox, canWarmup = true }: { form: Inbox; update: (p: Partial<Inbox>) => void; status?: AccountStatusModel; mailbox: Inbox; canWarmup?: boolean }) {
    const ws = status?.warmup_status;
    const wh = status?.warmup_health;
    const inCampaign = status?.in_campaign;

    // Warmup on/off is a lifecycle action (immediate), not a saved form field —
    // read live state off the mailbox prop, which the lifecycle mutation patches
    // back into cache on success.
    const life = useWarmupLifecycle(mailbox.id);
    const confirm = useConfirm();
    const off = !mailbox.warmup;
    const paused = !!mailbox.warmup && !!mailbox.warmup_paused_at;
    const active = !!mailbox.warmup && !mailbox.warmup_paused_at;
    // When Warmbly Cloud warms this mailbox the local controls step aside.
    const pool = useCloudPool();
    const inCloud = pool.connected && pool.isEnrolled(mailbox.id);

    const run = (action: "start" | "pause" | "resume" | "stop", verb: string) =>
        life.mutate(action, {
            onSuccess: () => toast.success(`Warmup ${verb}`),
            onError: (e) => toast.error(buildError(e as unknown as AppError)),
        });

    const stopReset = () => {
        confirm.show(
            "Stop warmup and reset ramp progress? Restarting begins from the base volume. Use Pause to keep progress.",
            async () => {
                try {
                    await life.mutateAsync("stop");
                    toast.success("Warmup stopped");
                } catch (e) {
                    toast.error(buildError(e as unknown as AppError));
                }
            },
        );
    };

    const baseOverMax = form.warmup_base > form.warmup_max;

    return (
        <div className="divide-y divide-slate-200/60">
            {/* Ban banner + appeal — only when the mailbox is blocked from the pool */}
            <WarmupBanBanner emailId={mailbox.id} />

            <CloudWarmupCard mailboxId={mailbox.id} email={mailbox.email} provider={mailbox.provider} />

            {/* Lifecycle control */}
            {!inCloud && (
            <div className="px-5 py-4 flex items-center justify-between gap-3">
                <div className="flex items-center gap-2.5 min-w-0">
                    <div className={cn("w-8 h-8 rounded-lg flex items-center justify-center shrink-0", active ? "bg-orange-50 text-orange-600" : paused ? "bg-amber-50 text-amber-600" : "bg-slate-100 text-slate-400")}>
                        <FlameIcon className="w-4 h-4" />
                    </div>
                    <div className="min-w-0">
                        <div className="text-[12.5px] font-medium text-slate-900">{active ? "Warming up" : paused ? "Paused" : "Warmup off"}</div>
                        <div className="text-[11px] text-slate-400 truncate">{active ? "Building sender reputation" : paused ? "Ramp progress kept — resume anytime" : "Not building reputation"}</div>
                    </div>
                </div>
                <div className="flex items-center gap-2 shrink-0">
                    {(off || paused) && canWarmup && (
                        <button
                            onClick={() => run(off ? "start" : "resume", off ? "started" : "resumed")}
                            disabled={life.isPending}
                            className="h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {life.isPending ? <Loading className="!w-3.5 h-3.5 text-white" /> : <PlayIcon className="w-3.5 h-3.5" />}
                            {off ? "Start" : "Resume"}
                        </button>
                    )}
                    {active && (
                        <>
                            <button
                                onClick={() => run("pause", "paused")}
                                disabled={life.isPending}
                                className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {life.isPending ? <Loading className="!w-3.5 h-3.5" /> : <PauseIcon className="w-3.5 h-3.5" />}
                                Pause
                            </button>
                            <button
                                onClick={stopReset}
                                disabled={life.isPending}
                                title="Stop warmup and reset ramp progress"
                                className="h-8 px-3 rounded-md border border-slate-200 hover:border-rose-200 text-[12px] font-medium text-slate-600 hover:text-rose-600 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                Stop
                            </button>
                        </>
                    )}
                    {paused && (
                        <button
                            onClick={stopReset}
                            disabled={life.isPending}
                            title="Stop warmup and reset ramp progress"
                            className="h-8 px-3 rounded-md border border-slate-200 hover:border-rose-200 text-[12px] font-medium text-slate-600 hover:text-rose-600 inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            Stop
                        </button>
                    )}
                </div>
            </div>
            )}

            {/* Upsell when warmup isn't available on the plan */}
            {!inCloud && off && !canWarmup && (
                <div className="px-5 py-4">
                    <div className="rounded-md border border-sky-100 bg-sky-50/70 px-3 py-2.5 text-[11.5px] text-sky-900/90 leading-relaxed">
                        Warmup is available on paid plans. Upgrade to build and protect sender reputation automatically.
                    </div>
                </div>
            )}

            {/* Live volume */}
            {ws && active && (
                <div className="px-5 py-4">
                    <Eyebrow>Today</Eyebrow>
                    <div className="mt-2 flex items-center gap-2 text-[12.5px] text-slate-700">
                        <span>Sending <b className="text-slate-900 tabular-nums">{ws.current_volume}</b> of <b className="text-slate-900 tabular-nums">{ws.target_volume}</b> · day {ws.days_active}</span>
                    </div>
                    <div className="mt-2.5 h-1.5 w-full rounded-full bg-slate-100 overflow-hidden">
                        <div className="h-full rounded-full bg-orange-400 transition-all" style={{ width: `${Math.min(100, (ws.current_volume / Math.max(1, ws.target_volume)) * 100)}%` }} />
                    </div>
                </div>
            )}

            {/* Warmup reputation (pool health) */}
            {wh && (
                <div className="px-5 py-4">
                    <div className="flex items-center justify-between">
                        <Eyebrow>Warmup reputation</Eyebrow>
                        <span className={cn("inline-flex items-center gap-1 text-[11px] font-medium", warmupStateTone[wh.state]?.text ?? "text-slate-500")}>
                            <ShieldCheckIcon className="w-3.5 h-3.5" /> {warmupStateTone[wh.state]?.label ?? wh.state}
                        </span>
                    </div>
                    {wh.reason && <p className="mt-1.5 text-[11.5px] text-slate-500 leading-relaxed">{wh.reason}</p>}
                    {wh.blocked_until && (
                        <p className="mt-1 text-[11px] text-rose-600">Paused from the pool until {new Date(wh.blocked_until).toLocaleDateString()}.</p>
                    )}
                </div>
            )}

            {/* Domain authentication (live SPF/DKIM/DMARC check) */}
            <AuthCheckPanel mailbox={mailbox} />

            {/* In-campaign health-check explainer */}
            {inCampaign && (
                <div className="px-5 py-4">
                    <div className="rounded-md border border-sky-100 bg-sky-50/70 px-3 py-2.5 flex gap-2.5">
                        <ShieldCheckIcon className="w-4 h-4 text-sky-600 shrink-0 mt-0.5" />
                        <p className="text-[11.5px] text-sky-900/90 leading-relaxed">
                            This mailbox is active in a campaign. Warmbly keeps a low volume of
                            {" "}<b>health-check warmup (~5/day)</b> flowing{active ? "" : " even while warmup is off"} so we can
                            continuously watch deliverability while it sends cold outreach.
                        </p>
                    </div>
                </div>
            )}

            {/* Ramp configuration */}
            <div className="px-5 py-5 space-y-5">
                <Eyebrow>Ramp configuration</Eyebrow>
                <FieldShell label="Starting volume" hint="Emails per day when warmup begins.">
                    <NumField value={form.warmup_base} onChange={(v) => update({ warmup_base: v })} suffix="emails / day" />
                </FieldShell>
                <FieldShell label="Daily increase" hint="How many more emails to send each day as reputation builds.">
                    <NumField value={form.warmup_increase} onChange={(v) => update({ warmup_increase: v })} suffix="+ / day" />
                </FieldShell>
                <FieldShell label="Maximum volume" hint="Ceiling the ramp grows toward. Keep conservative for new mailboxes (≈40/day).">
                    <NumField value={form.warmup_max} onChange={(v) => update({ warmup_max: v })} suffix="emails / day" />
                </FieldShell>
                {baseOverMax && (
                    <p className="text-[11px] text-rose-600 -mt-3">Starting volume can't exceed the maximum.</p>
                )}
                <FieldShell label="Reply rate" hint="Share of warmup mail that gets a reply, to mimic real conversation.">
                    <NumField value={form.warmup_reply_rate} onChange={(v) => update({ warmup_reply_rate: v })} suffix="%" />
                </FieldShell>
                <FieldShell label="Content segment" hint="Targets segment-specific warmup content (e.g. saas, agency). Leave empty for generic content.">
                    <TextInput
                        value={form.warmup_tag ?? ""}
                        placeholder="e.g. saas, agency"
                        onChange={(v) => update({ warmup_tag: v.toLowerCase().replace(/[^a-z0-9-]/g, "") })}
                        className="w-full h-9"
                    />
                </FieldShell>
            </div>

            {/* Schedule */}
            <div className="px-5 py-5 space-y-5">
                <Eyebrow>Sending window</Eyebrow>
                <div className="grid grid-cols-2 gap-3">
                    <FieldShell label="Start time">
                        <TimeSelect value={form.warmup_start_time || "08:00"} onChange={(v) => update({ warmup_start_time: v })} />
                    </FieldShell>
                    <FieldShell label="End time">
                        <TimeSelect value={form.warmup_end_time || "20:00"} onChange={(v) => update({ warmup_end_time: v })} />
                    </FieldShell>
                </div>
                <FieldShell label="Sending days" hint="Days warmup mail goes out. Leave all unselected to send every day.">
                    <div className="mt-1">
                        <WeekdayBitmask
                            weekdays={WEEKDAYS}
                            value={form.warmup_days ?? 0}
                            setValue={(v) => update({ warmup_days: v })}
                        />
                    </div>
                </FieldShell>
            </div>
        </div>
    );
}

/* ── Tracking domain (own save + DNS verify flow) ─────────────────────── */

// normalizeTrackingDomain mirrors the backend: accept whatever is pasted (a
// full URL, a trailing dot, stray case) and keep the bare host. Without this a
// pasted link saved as-is and then sat at "Pending DNS" forever, which is what
// made this look broken rather than wrong.
function normalizeTrackingDomain(raw: string): string {
    let v = raw.trim();
    const scheme = v.indexOf("://");
    if (scheme >= 0) v = v.slice(scheme + 3);
    const at = v.lastIndexOf("@");
    if (at >= 0) v = v.slice(at + 1);
    const cut = v.search(/[/?#]/);
    if (cut >= 0) v = v.slice(0, cut);
    return v.trim().toLowerCase().replace(/\.+$/, "");
}

// A hostname with at least one dot and an alphabetic TLD, matching
// validate.TrackingHostname on the backend so the error arrives before the
// round trip instead of after it.
function trackingDomainProblem(host: string): string | null {
    if (!host) return null;
    if (host.length > 253) return "That domain is too long.";
    if (/[^a-z0-9.-]/.test(host)) return "Use a plain hostname, like track.yourdomain.com.";
    if (!host.includes(".")) return "Use a subdomain of a domain you own, like track.yourdomain.com.";
    if (host.split(".").some((l) => !l || l.startsWith("-") || l.endsWith("-"))) {
        return "Use a plain hostname, like track.yourdomain.com.";
    }
    if (!/^[a-z]{2,}$/.test(host.split(".").pop() ?? "")) return "That does not end in a domain ending, like .com.";
    return null;
}

function TrackingDomainCard({ mailbox }: { mailbox: Inbox }) {
    const [domain, setDomain] = useState(mailbox.tracking_domain ?? "");
    const [copied, setCopied] = useState(false);
    const status = useEmailTrackingDomain(mailbox.id);
    const mutation = useUpdateEmailTrackingDomain(mailbox.id);
    const verify = useVerifyEmailTrackingDomain(mailbox.id);

    const saved = (status.data?.tracking_domain ?? mailbox.tracking_domain ?? "").trim();
    const verified = status.data?.tracking_domain_verified ?? mailbox.tracking_domain_verified;
    const normalized = normalizeTrackingDomain(domain);
    const dirty = normalized !== saved;
    const problem = trackingDomainProblem(normalized);
    // The target is this install's own tracking host, so a self-hosted
    // deployment shows its own value instead of the cloud one.
    const target = status.data?.cname_target ?? "";
    const busy = mutation.isPending || verify.isPending;
    // Show the diagnostic while something is wrong, not once it is fixed.
    const message = !verified && status.data?.message && !dirty ? status.data.message : "";

    const copyTarget = async () => {
        if (!target) return;
        try {
            await navigator.clipboard.writeText(target);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
        } catch {
            // clipboard may be unavailable (insecure context); ignore
        }
    };

    const save = async () => {
        if (problem) return;
        try {
            const res = await mutation.mutateAsync(normalized);
            setDomain(res.tracking_domain);
            if (!normalized) {
                toast.success("Tracking domain cleared");
            } else if (res.tracking_domain_verified) {
                toast.success(res.tracking_host_unresolvable ? "Saved, but the tracking host is not answering" : "Tracking domain verified");
            } else {
                toast(res.message || "Saved. DNS hasn't propagated yet, check again in a few minutes.", { icon: "⏳" });
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const recheck = async () => {
        try {
            const res = await verify.mutateAsync();
            if (res.tracking_domain_verified) {
                toast.success(res.tracking_host_unresolvable ? "Your record is correct, but the tracking host is not answering" : "Tracking domain verified");
            } else {
                toast(res.message || "Still waiting on DNS.", { icon: "⏳" });
            }
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    const clear = async () => {
        setDomain("");
        try {
            await mutation.mutateAsync("");
            toast.success("Tracking domain cleared");
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    return (
        <div className="px-5 py-5 space-y-3">
            <div className="flex items-center justify-between">
                <Eyebrow>Tracking domain</Eyebrow>
                {saved ? (
                    verified ? (
                        <span className="inline-flex items-center gap-1 h-5 px-2 rounded-full border border-emerald-100 bg-emerald-50 text-emerald-700 text-[10px] font-semibold uppercase tracking-wide">
                            <CheckCircle2Icon className="w-3 h-3" /> Verified
                        </span>
                    ) : (
                        <span className="inline-flex items-center gap-1 h-5 px-2 rounded-full border border-amber-100 bg-amber-50 text-amber-700 text-[10px] font-semibold uppercase tracking-wide">
                            <ClockIcon className="w-3 h-3" /> Pending DNS
                        </span>
                    )
                ) : (
                    <span className="inline-flex items-center gap-1 h-5 px-2 rounded-full border border-slate-200 bg-slate-100 text-slate-500 text-[10px] font-semibold uppercase tracking-wide">
                        Not set
                    </span>
                )}
            </div>

            <FieldShell label="Custom tracking domain" hint="Track opens & clicks through your own subdomain instead of the shared host. Improves deliverability.">
                <TextInput value={domain} placeholder="track.yourdomain.com" onChange={setDomain} className="w-full h-9" />
            </FieldShell>

            {problem && (
                <div className="flex items-start gap-1.5 text-[11.5px] text-rose-600">
                    <AlertCircleIcon className="w-3.5 h-3.5 shrink-0 mt-px" />
                    <span>{problem}</span>
                </div>
            )}

            {/* Nothing to point at: the install itself has no tracking host. */}
            {!problem && normalized && status.data && !target && (
                <div className="flex items-start gap-1.5 text-[11.5px] text-amber-700">
                    <AlertTriangleIcon className="w-3.5 h-3.5 shrink-0 mt-px" />
                    <span>This Warmbly install has no tracking host configured, so a custom domain cannot be verified yet. Ask your administrator to set TRACKING_DOMAIN.</span>
                </div>
            )}

            {!problem && normalized && target && (
                <div className="rounded-md border border-slate-200 bg-slate-50/70 p-3 space-y-2">
                    <div className="text-[11px] text-slate-500 leading-relaxed">
                        Add this CNAME record at your DNS provider, then save to verify:
                    </div>
                    <div className="grid grid-cols-[56px_1fr] gap-x-3 gap-y-1.5 text-[11.5px] items-center">
                        <span className="text-slate-400">Type</span>
                        <span className="font-mono text-slate-700">CNAME</span>
                        <span className="text-slate-400">Name</span>
                        <span className="font-mono text-slate-700 truncate">{normalized}</span>
                        <span className="text-slate-400">Value</span>
                        <span className="font-mono text-slate-700 inline-flex items-center gap-1.5 min-w-0">
                            <span className="truncate">{target}</span>
                            <button
                                type="button"
                                onClick={copyTarget}
                                className="shrink-0 text-slate-400 hover:text-slate-700 transition-colors"
                                aria-label="Copy CNAME target"
                            >
                                {copied ? <CheckIcon className="w-3 h-3 text-emerald-600" /> : <CopyIcon className="w-3 h-3" />}
                            </button>
                        </span>
                    </div>
                </div>
            )}

            {/* Why it is not verified, in the customer's own terms. */}
            {message && (
                <div className="flex items-start gap-1.5 text-[11.5px] text-slate-500 leading-relaxed">
                    <AlertCircleIcon className="w-3.5 h-3.5 shrink-0 mt-px text-amber-500" />
                    <span>
                        {message}
                        {status.data?.observed && status.data.status === "wrong_target" && (
                            <> Found <span className="font-mono text-slate-600">{status.data.observed}</span>.</>
                        )}
                    </span>
                </div>
            )}

            {/* Verified but dead: the customer's record is right and ours is not. */}
            {verified && status.data?.tracking_host_unresolvable && (
                <div className="flex items-start gap-1.5 text-[11.5px] text-amber-700 leading-relaxed">
                    <AlertTriangleIcon className="w-3.5 h-3.5 shrink-0 mt-px" />
                    <span>{status.data.message}</span>
                </div>
            )}

            {/* One primary action, whichever one applies: save what was typed,
                re-resolve what is already saved, or nothing left to do. */}
            <div className="flex items-center gap-2">
                {dirty || !saved ? (
                    <button
                        onClick={save}
                        disabled={busy || !!problem || (!dirty && !saved)}
                        className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {mutation.isPending && <Loading className="!w-3.5 h-3.5 text-white" />}
                        Save &amp; verify
                    </button>
                ) : verified ? (
                    <button
                        disabled
                        className="h-8 px-3.5 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 disabled:opacity-60"
                    >
                        <CheckCircle2Icon className="w-3.5 h-3.5" /> Verified
                    </button>
                ) : (
                    <button
                        onClick={recheck}
                        disabled={busy}
                        className="h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {verify.isPending ? <Loading className="!w-3.5 h-3.5 text-white" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                        Check again
                    </button>
                )}
                {saved && !dirty && (
                    <button
                        onClick={clear}
                        disabled={busy}
                        className="h-8 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-600 hover:text-slate-900 transition-colors disabled:opacity-60"
                    >
                        Clear
                    </button>
                )}
            </div>
        </div>
    );
}

/* ── Settings (editable) ─────────────────────── */

function SettingsTab({ form, update, mailbox }: { form: Inbox; update: (p: Partial<Inbox>) => void; mailbox: Inbox }) {
    return (
        <div className="divide-y divide-slate-200/60">
            <div className="px-5 py-5 space-y-4">
                <Eyebrow>Sender profile</Eyebrow>
                <FieldShell label="Display name">
                    <TextInput value={form.name ?? ""} placeholder="First Last" onChange={(v) => update({ name: v })} className="w-full h-9" />
                </FieldShell>
                <FieldShell
                    label="Reply-to"
                    hint={`Where replies land. Leave empty to use ${mailbox.email}.`}
                >
                    <div className="flex items-center gap-1.5">
                        <TextInput
                            value={form.reply_to ?? ""}
                            placeholder={mailbox.email}
                            onChange={(v) => update({ reply_to: v })}
                            className="w-full h-9"
                        />
                        {form.reply_to !== mailbox.email && (
                            <button
                                type="button"
                                onClick={() => update({ reply_to: mailbox.email })}
                                className="h-9 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors shrink-0"
                            >
                                Use mailbox address
                            </button>
                        )}
                    </div>
                </FieldShell>
            </div>

            {mailbox.provider === "smtp_imap" && (
                <div className="px-5 py-5 space-y-3">
                    <Eyebrow>Sent folder</Eyebrow>
                    <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0">
                            <div className="text-[12.5px] font-medium text-slate-900">
                                Keep a copy of sent mail
                            </div>
                            <div className="text-[11px] text-slate-400">
                                Files each message Warmbly sends into this mailbox's Sent
                                folder. Turn it off if your provider already saves one, or
                                you will see every message twice.
                            </div>
                        </div>
                        <Toggle
                            value={form.save_to_sent ?? true}
                            onChange={(v) => update({ save_to_sent: v })}
                        />
                    </div>
                </div>
            )}

            <div className="px-5 py-5 space-y-2">
                <Eyebrow>Signature</Eyebrow>
                <div className="overflow-x-auto">
                    <EmailEditor
                        id="inbox-signature"
                        htmlText={form.signature_html}
                        setHtmlText={(v) => update({ signature_html: v })}
                        plainText={form.signature_plain}
                        setPlainText={(v) => update({ signature_plain: v })}
                        sync={form.signature_sync}
                        setSync={(v) => update({ signature_sync: v })}
                        code={form.signature_code}
                        setCode={(v) => update({ signature_code: v })}
                    />
                </div>
            </div>

            <div className="px-5 py-5 space-y-2">
                <Eyebrow>Tags</Eyebrow>
                <TagSelector
                    selected={form.tags}
                    onAdd={(v) => update({ tags: [...form.tags, v] })}
                    onRemove={(v) => update({ tags: form.tags.filter((t) => t !== v) })}
                />
            </div>

            <div className="px-5 py-5 space-y-5">
                <Eyebrow>Sending limits</Eyebrow>
                <FieldShell label="Daily campaign cap" hint="Max cold-campaign emails per day, up to 5,000. Default 50; raise only with good reputation.">
                    <NumField value={form.campaign_limit} onChange={(v) => update({ campaign_limit: v })} suffix="emails / day" max={5000} />
                    {form.campaign_limit > 100 && (
                        <p className="text-[11px] text-amber-600 mt-1 leading-relaxed">
                            Well above the 30–50/day safe band for cold outreach. Caps this high need a warmed,
                            established mailbox and a provider that allows the volume (Google Workspace tops out at
                            2,000/day). Deliverability damage shows up as spam placement, not as errors.
                        </p>
                    )}
                </FieldShell>
                <FieldShell
                    label="Minimum gap"
                    hint={`Smallest delay between two sends from this mailbox — currently ${formatGap(form.min_wait_time)}. Overridden while Sending behaviour is on, which draws a fresh delay for every send.`}
                >
                    <NumField value={form.min_wait_time} onChange={(v) => update({ min_wait_time: v })} suffix="seconds" max={86400} />
                </FieldShell>
            </div>

            <TrackingDomainCard mailbox={mailbox} />

            <div className="flex flex-wrap items-center gap-1.5 px-5 py-3 text-[11px] text-slate-400">
                <SendIcon className="w-3 h-3" /> Changes apply to new sends. <ReplyIcon className="w-3 h-3 ml-1" /> Signature applies to replies too.
            </div>
        </div>
    );
}
