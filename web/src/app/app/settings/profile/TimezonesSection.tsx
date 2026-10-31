// Every clock the workspace runs on, in one place: the browser's (display
// only), the workspace's (the default everything else falls back to), and
// the per-campaign and per-mailbox zones, each editable inline.
import React from "react";
import { Link } from "@tanstack/react-router";
import toast from "react-hot-toast/headless";
import { useQueryClient } from "@tanstack/react-query";
import { CheckIcon, ChevronDownIcon, ClockIcon, InboxIcon, MegaphoneIcon, UsersIcon } from "lucide-react";
import { Row, Section } from "../_components/SectionShell";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { Loading } from "@/components/loader";
import { useUserProfile } from "@/hooks/context/user";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission } from "@/hooks/usePermission";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import useUpdateOrganization from "@/lib/api/hooks/app/organizations/useUpdateOrganization";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import updateCampaign from "@/lib/api/client/app/campaigns/updateCampaign";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import useUpdateEmail from "@/lib/api/hooks/app/emails/useUpdateEmail";
import updateEmail from "@/lib/api/client/app/emails/updateEmail";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useCloudPool from "@/hooks/useCloudPool";
import { resolveMailboxWarmup, warmupActivityLabel, type MailboxWarmup } from "@/lib/mailboxWarmup";
import { browserTimezone, followWorkspaceLabel, timezoneOptions } from "@/lib/timezone";
import { cn } from "@/lib/utils";

const CAMPAIGN_TONE: Record<string, string> = {
    active: "bg-emerald-50 text-emerald-700 ring-emerald-200",
    paused: "bg-amber-50 text-amber-700 ring-amber-200",
    draft: "bg-slate-100 text-slate-600 ring-slate-200",
};

const MAILBOX_DOT: Record<string, string> = {
    healthy: "bg-emerald-500",
    warming: "bg-sky-500",
    paused: "bg-amber-500",
    inactive: "bg-slate-300",
};

export default function TimezonesSection() {
    const canManageSettings = usePermission("MANAGE_SETTINGS");
    const org = useCurrentOrganization();
    const updateOrg = useUpdateOrganization();
    const { timezones } = useUserProfile();
    const workspaceZone = org.data?.timezone ?? "";
    const browserZone = browserTimezone();
    const options = React.useMemo<SelectOption[]>(
        () => timezoneOptions(timezones, workspaceZone, browserZone),
        [timezones, workspaceZone, browserZone],
    );
    const saveWorkspace = (zone: string) => {
        if (zone === workspaceZone) return;
        updateOrg.mutateAsync({ timezone: zone }).catch((e) => toast.error(buildError(e as AppError)));
    };

    return (
        <Section eyebrow="Timezones" description="Every clock this workspace runs on, and where each one is set.">
            <Row
                label="Your browser"
                description="Detected from this device. Dates and times across the dashboard are shown in it. Nothing is sent on it."
                align="start"
            >
                <ZoneChip zone={browserZone || "Unknown"} />
            </Row>
            <Row
                label="Workspace"
                description={
                    canManageSettings
                        ? "Shared by everyone here. New campaigns start their sending window in it, and a mailbox without a timezone of its own reads its warmup hours and working hours in it."
                        : "Shared by everyone here. New campaigns start their sending window in it, and a mailbox without a timezone of its own reads its warmup hours and working hours in it. Changing it needs the Manage settings permission."
                }
                align="start"
            >
                <div className="flex flex-col items-end gap-1.5">
                    <SelectMenu
                        value={workspaceZone}
                        onChange={saveWorkspace}
                        options={options}
                        placeholder="Not set"
                        aria-label="Workspace timezone"
                        minWidth={280}
                        align="end"
                        disabled={!canManageSettings || org.isPending}
                    />
                    {!workspaceZone && browserZone && canManageSettings && (
                        <button
                            type="button"
                            onClick={() => saveWorkspace(browserZone)}
                            className="text-[11.5px] text-sky-700 hover:text-sky-900 hover:underline"
                        >
                            Use {browserZone}, this browser&apos;s timezone
                        </button>
                    )}
                </div>
            </Row>

            <div className="rounded-md border border-slate-200 divide-y divide-slate-200 overflow-hidden">
                <CampaignClocks workspaceZone={workspaceZone} />
                <MailboxClocks workspaceZone={workspaceZone} />
                <ClockRow
                    icon={UsersIcon}
                    title="Recipient timing"
                    description="Holds each send for the contact's own local hours when enabled. It only ever delays a send."
                    chips={
                        <Link
                            to="/app/settings/sending"
                            className="h-7 px-2.5 inline-flex items-center rounded-md border border-slate-200 text-[12px] text-slate-600 hover:border-slate-300 hover:text-slate-900 transition-colors"
                        >
                            Sending settings
                        </Link>
                    }
                />
                <ClockRow
                    icon={ClockIcon}
                    title="Daily budgets"
                    description="Every daily counter resets at midnight UTC whatever the zones above, so a workspace far from UTC sees “sent today” reset partway through its own day."
                    chips={<ZoneChip zone="UTC" />}
                />
            </div>
        </Section>
    );
}

/* ── Campaigns ─────────────────────────────────────────────────────── */

function CampaignClocks({ workspaceZone }: { workspaceZone: string }) {
    const list = useCampaigns({ query: "", folder: "" });
    const loading = list.isLoadingRest || list.isPending;
    const queryClient = useQueryClient();
    const confirm = useConfirm();
    const [open, setOpen] = React.useState(false);
    const [busy, setBusy] = React.useState(false);

    // A finished campaign has no window left to move.
    const campaigns = React.useMemo(() => list.campaigns.filter((c) => c.status !== "completed"), [list.campaigns]);
    const own = campaigns.filter((c) => !!c.timezone);
    const fallback = workspaceZone || "UTC";
    const zones = summarizeZones(campaigns.map((c) => c.timezone || ""));

    const followAll = () => {
        confirm.show(
            `Have ${own.length} campaign${own.length === 1 ? "" : "s"} follow the workspace timezone (${fallback})? Each sending window keeps its hours and is read in that zone from its next send.`,
            async () => {
                setBusy(true);
                try {
                    for (const c of own) await updateCampaign(c.id, { timezone: "" });
                    toast.success("Campaigns now follow the workspace timezone");
                } catch (e) {
                    toast.error(buildError(e as AppError));
                } finally {
                    setBusy(false);
                    void queryClient.invalidateQueries({ queryKey: ["campaigns"] });
                }
            },
        );
    };

    return (
        <ClockRow
            icon={MegaphoneIcon}
            title="Campaign sending windows"
            description={`Each campaign's window is read in its own timezone or, with none set, the workspace's (${fallback} right now). Change it here or on the campaign's Schedule tab.`}
            summary={loading ? undefined : summaryLabel(campaigns.length, "campaign", zones)}
            open={open}
            onToggle={!loading && campaigns.length > 0 ? () => setOpen((v) => !v) : undefined}
            chips={
                loading ? (
                    <Loading className="!w-4 h-4" />
                ) : campaigns.length === 0 ? (
                    <span className="text-[11.5px] text-slate-400">No campaigns yet</span>
                ) : (
                    <ZoneChips zones={zones} highlight={workspaceZone} followLabel={`Follow workspace (${fallback})`} />
                )
            }
        >
            {open && !loading && campaigns.length > 0 && (
                <ClockList
                    action={
                        own.length > 0 ? (
                            <AlignButton busy={busy} onClick={followAll}>
                                Set all {own.length} to follow the workspace
                            </AlignButton>
                        ) : (
                            <AllAligned>All follow the workspace</AllAligned>
                        )
                    }
                >
                    {campaigns.map((c) => (
                        <CampaignZoneRow key={c.id} campaign={c} workspaceZone={workspaceZone} fallback={fallback} />
                    ))}
                </ClockList>
            )}
        </ClockRow>
    );
}

function CampaignZoneRow({ campaign, workspaceZone, fallback }: { campaign: Campaign; workspaceZone: string; fallback: string }) {
    const { timezones } = useUserProfile();
    const update = useUpdateCampaign(campaign.id);
    const options = React.useMemo<SelectOption[]>(
        () => [{ value: "", label: followWorkspaceLabel(fallback) }, ...timezoneOptions(timezones, campaign.timezone)],
        [timezones, campaign.timezone, fallback],
    );
    const differs = !!campaign.timezone && !!workspaceZone && campaign.timezone !== workspaceZone;
    const onChange = (zone: string) => {
        if (zone === (campaign.timezone || "")) return;
        update.mutateAsync({ timezone: zone }).catch((e) => toast.error(buildError(e as AppError)));
    };
    return (
        <li className="flex items-center gap-3 px-3.5 py-2">
            <div className="min-w-0 flex-1 flex items-center gap-2">
                <Link to="/app/campaigns/$id/schedule" params={{ id: campaign.id }} className="truncate text-[12.5px] text-slate-800 hover:text-sky-700 hover:underline">
                    {campaign.name || "Untitled campaign"}
                </Link>
                <span className={cn("shrink-0 px-1.5 py-px rounded text-[10px] font-medium ring-1", CAMPAIGN_TONE[campaign.status] ?? CAMPAIGN_TONE.draft)}>
                    {campaign.status}
                </span>
                {differs && <span className="shrink-0 text-[10.5px] text-amber-600">own timezone</span>}
            </div>
            <SelectMenu
                value={campaign.timezone || ""}
                onChange={onChange}
                options={options}
                aria-label={`Timezone of ${campaign.name}`}
                minWidth={260}
                align="end"
                disabled={update.isPending}
            />
        </li>
    );
}

/* ── Mailboxes ─────────────────────────────────────────────────────── */

function MailboxClocks({ workspaceZone }: { workspaceZone: string }) {
    const pool = useCloudPool();
    const list = useEmails({ query: "", tag: "" });
    const loading = list.isLoadingRest || list.isPending;
    const queryClient = useQueryClient();
    const confirm = useConfirm();
    const [open, setOpen] = React.useState(false);
    const [busy, setBusy] = React.useState(false);

    const mailboxes = list.emails;
    const own = mailboxes.filter((m) => !!m.timezone);
    const fallback = workspaceZone || "UTC";
    const zones = summarizeZones(mailboxes.map((m) => m.timezone || ""));

    const followAll = () => {
        confirm.show(
            `Have ${own.length} mailbox${own.length === 1 ? "" : "es"} follow the workspace timezone (${fallback})? Their warmup hours and working hours are read in it from the next send.`,
            async () => {
                setBusy(true);
                try {
                    for (const m of own) await updateEmail(m.id, { timezone: "" });
                    toast.success("Mailboxes now follow the workspace timezone");
                } catch (e) {
                    toast.error(buildError(e as AppError));
                } finally {
                    setBusy(false);
                    void queryClient.invalidateQueries({ queryKey: ["emails"] });
                }
            },
        );
    };

    return (
        <ClockRow
            icon={InboxIcon}
            title="Warmup hours and working hours"
            description={`Each mailbox's warmup window and its Sending behaviour workday are read in its own timezone. A mailbox without one follows the workspace, which is ${fallback} right now.`}
            summary={loading ? undefined : summaryLabel(mailboxes.length, "mailbox", zones)}
            open={open}
            onToggle={!loading && mailboxes.length > 0 ? () => setOpen((v) => !v) : undefined}
            chips={
                loading ? (
                    <Loading className="!w-4 h-4" />
                ) : mailboxes.length === 0 ? (
                    <span className="text-[11.5px] text-slate-400">No mailboxes yet</span>
                ) : (
                    <ZoneChips zones={zones} highlight={workspaceZone} followLabel={`Follow workspace (${fallback})`} />
                )
            }
        >
            {open && !loading && mailboxes.length > 0 && (
                <ClockList
                    action={
                        own.length > 0 ? (
                            <AlignButton busy={busy} onClick={followAll}>
                                Set all {own.length} to follow the workspace
                            </AlignButton>
                        ) : (
                            <AllAligned>All follow the workspace</AllAligned>
                        )
                    }
                >
                    {mailboxes.map((m) => (
                        <MailboxZoneRow key={m.id} mailbox={m} fallback={fallback} warmup={resolveMailboxWarmup(m, undefined, pool)} />
                    ))}
                </ClockList>
            )}
        </ClockRow>
    );
}

function MailboxZoneRow({ mailbox, fallback, warmup }: { mailbox: Inbox; fallback: string; warmup: MailboxWarmup }) {
    const { timezones } = useUserProfile();
    const update = useUpdateEmail(mailbox.id);
    const options = React.useMemo<SelectOption[]>(
        () => [{ value: "", label: followWorkspaceLabel(fallback) }, ...timezoneOptions(timezones, mailbox.timezone)],
        [timezones, mailbox.timezone, fallback],
    );
    const zone = mailbox.timezone ?? "";
    const onChange = (next: string) => {
        if (next === zone) return;
        update.mutateAsync({ timezone: next }).catch((e) => toast.error(buildError(e as AppError)));
    };
    const status = warmup.activity === "active" ? "warming" : warmup.activity === "paused" || warmup.activity === "receive_only" ? "paused" : warmup.activity === "off" && mailbox.status === "active" ? "healthy" : "inactive";
    return (
        <li className="flex items-center gap-3 px-3.5 py-2">
            <span className={cn("shrink-0 w-1.5 h-1.5 rounded-full", MAILBOX_DOT[status])} title={`${warmup.source === "cloud" ? "Warmbly Cloud: " : ""}${warmupActivityLabel[warmup.activity]}`} />
            <div className="min-w-0 flex-1 flex items-baseline gap-2">
                <span className="truncate text-[12.5px] text-slate-800">{mailbox.email}</span>
                {mailbox.name && <span className="truncate text-[11px] text-slate-400 hidden sm:inline">{mailbox.name}</span>}
            </div>
            <SelectMenu
                value={zone}
                onChange={onChange}
                options={options}
                aria-label={`Timezone of ${mailbox.email}`}
                minWidth={260}
                align="end"
                disabled={update.isPending}
            />
        </li>
    );
}

/* ── Shared pieces ─────────────────────────────────────────────────── */

type ZoneCount = { zone: string; count: number };

// Grouped counts, most common first; "" is "follows the workspace".
function summarizeZones(zones: string[]): ZoneCount[] {
    const counts = new Map<string, number>();
    for (const z of zones) counts.set(z, (counts.get(z) ?? 0) + 1);
    return [...counts.entries()].map(([zone, count]) => ({ zone, count })).sort((a, b) => b.count - a.count);
}

function summaryLabel(total: number, noun: string, zones: ZoneCount[]): string {
    const plural = noun === "mailbox" ? "mailboxes" : `${noun}s`;
    if (total === 0) return `No ${plural}`;
    const following = zones.find((z) => z.zone === "")?.count ?? 0;
    if (following === total) return `${total} ${total === 1 ? noun : plural}, all following the workspace`;
    return `${total} ${plural}, ${following} following the workspace, ${total - following} with their own`;
}

function ClockRow({
    icon: Icon,
    title,
    description,
    summary,
    open,
    onToggle,
    chips,
    children,
}: {
    icon: React.ComponentType<{ className?: string }>;
    title: string;
    description: string;
    summary?: string;
    open?: boolean;
    onToggle?: () => void;
    // What sits at the right of the header: a chip, a link, the zone counts.
    chips: React.ReactNode;
    // The expanded list, when open.
    children?: React.ReactNode;
}) {
    const header = (
        <>
            <Icon className="w-4 h-4 text-slate-400 shrink-0 mt-0.5" />
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-baseline gap-x-2">
                    <span className="text-[12.5px] font-medium text-slate-900">{title}</span>
                    {summary && <span className="text-[11px] text-slate-400">{summary}</span>}
                </div>
                <p className="text-[11.5px] text-slate-500 leading-snug mt-0.5">{description}</p>
            </div>
            <div className="shrink-0 flex items-center gap-2 sm:ml-auto">
                {chips}
                {onToggle && (
                    <ChevronDownIcon className={cn("w-4 h-4 text-slate-400 transition-transform", open && "rotate-180")} />
                )}
            </div>
        </>
    );
    return (
        <div className="bg-white">
            {onToggle ? (
                <button
                    type="button"
                    onClick={onToggle}
                    aria-expanded={open}
                    className="w-full text-left flex flex-col sm:flex-row sm:items-start gap-2 sm:gap-3 px-3.5 py-3 hover:bg-slate-50/70 transition-colors"
                >
                    {header}
                </button>
            ) : (
                <div className="flex flex-col sm:flex-row sm:items-start gap-2 sm:gap-3 px-3.5 py-3">{header}</div>
            )}
            {children}
        </div>
    );
}

function ClockList({ action, children }: { action: React.ReactNode; children: React.ReactNode }) {
    return (
        <div className="border-t border-slate-200 bg-slate-50/50">
            {action && <div className="flex items-center justify-end px-3.5 py-2 border-b border-slate-200/70">{action}</div>}
            <ul className="divide-y divide-slate-200/70 max-h-[420px] overflow-y-auto">{children}</ul>
        </div>
    );
}

function ZoneChip({ zone, tone = "slate" }: { zone: string; tone?: "slate" | "sky" }) {
    return (
        <span
            className={cn(
                "inline-flex items-center h-6 px-2 rounded-md border text-[11px] font-mono",
                tone === "sky" ? "border-sky-200 bg-sky-50 text-sky-700" : "border-slate-200 bg-slate-50 text-slate-600",
            )}
        >
            {zone}
        </span>
    );
}

// The grouped zones as chips, the workspace's own zone in sky. At most three,
// then a "+N" so a scattered workspace does not blow the row up.
function ZoneChips({ zones, highlight, followLabel }: { zones: ZoneCount[]; highlight: string; followLabel?: string }) {
    const shown = zones.slice(0, 3);
    const rest = zones.length - shown.length;
    return (
        <div className="flex flex-wrap items-center justify-end gap-1">
            {shown.map(({ zone, count }) => (
                <span
                    key={zone || "__follow"}
                    className={cn(
                        "inline-flex items-center gap-1 h-6 px-2 rounded-md border text-[11px]",
                        (zone === "" || zone === highlight) && highlight
                            ? "border-sky-200 bg-sky-50 text-sky-700"
                            : "border-slate-200 bg-slate-50 text-slate-600",
                    )}
                >
                    <span className="font-mono">{zone || followLabel || "Follow workspace"}</span>
                    <span className="text-[10px] opacity-70">×{count}</span>
                </span>
            ))}
            {rest > 0 && <span className="text-[11px] text-slate-400">+{rest}</span>}
        </div>
    );
}

function AlignButton({ busy, onClick, children }: { busy: boolean; onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            type="button"
            onClick={onClick}
            disabled={busy}
            className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md bg-sky-600 text-white text-[12px] font-medium hover:bg-sky-700 disabled:opacity-60 transition-colors"
        >
            {busy && <Loading className="!w-3.5 h-3.5" />}
            {children}
        </button>
    );
}

function AllAligned({ children }: { children: React.ReactNode }) {
    return (
        <span className="inline-flex items-center gap-1 text-[11.5px] text-emerald-700">
            <CheckIcon className="w-3.5 h-3.5" /> {children}
        </span>
    );
}
