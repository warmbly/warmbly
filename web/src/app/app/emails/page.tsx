import { RiFireLine, RiMoreLine } from "@remixicon/react";
import React, { useEffect, useMemo, useRef } from "react";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import useWarmupLifecycle from "@/lib/api/hooks/app/emails/useWarmupLifecycle";
import useAccountStatuses from "@/lib/api/hooks/app/analytics/useAccountStatuses";
import useFeatureStatus from "@/lib/api/hooks/app/subscription/useFeatureStatus";
import warmupLifecycle from "@/lib/api/client/app/emails/warmupLifecycle";
import removeEmail from "@/lib/api/client/app/emails/removeEmail";
import { useUserProfile } from "@/hooks/context/user";
import { useConfirm } from "@/hooks/context/confirm";
import InboxDetails from "@/components/app/emails/InboxDetails";
import BulkWarmupDialog from "@/components/app/emails/BulkWarmupDialog";
import type Tag from "@/lib/api/models/app/Tag";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import {
    ActivityIcon,
    CheckIcon,
    FilterIcon,
    GaugeIcon,
    PauseIcon,
    PlayIcon,
    PlusIcon,
    RotateCcwIcon,
    Settings2Icon,
    Trash2Icon,
    XIcon,
} from "lucide-react";
import { SearchInput } from "@/components/ui/field";
import AnimatedNumber from "@/components/ui/AnimatedNumber";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";

const DefaultFolder = {
    title: "All accounts",
    color: "#c4c8cf",
} as Tag;

/* ── health helpers ───────────────────────────────── */

// Rank used to detect when a mailbox's health worsens between refreshes, so we
// can proactively toast the user (continuous health reporting).
const HEALTH_RANK: Record<string, number> = { healthy: 0, warning: 1, error: 2 };

function healthTone(status?: AccountStatus): { dot: string; text: string; label: string; pulse: boolean } {
    const h = status?.health;
    if (!h) return { dot: "bg-slate-300", text: "text-slate-500", label: "—", pulse: false };
    if (h.status === "healthy") return { dot: "bg-emerald-500", text: "text-emerald-600", label: `Healthy ${h.score}`, pulse: false };
    if (h.status === "warning") return { dot: "bg-amber-500", text: "text-amber-600", label: `At risk ${h.score}`, pulse: true };
    return { dot: "bg-rose-500", text: "text-rose-600", label: `Issue ${h.score}`, pulse: true };
}

export default function AddressesPage() {
    const p = useUserProfile();
    const confirm = useConfirm();

    const [query, setQuery] = React.useState<string>("");
    const [tag, setTag] = React.useState<string>("");
    const emailsData = useEmails({ query, tag });
    const [selected, setSelected] = React.useState<string[]>([]);
    const [view, setView] = React.useState<string>("");
    const [viewTab, setViewTab] = React.useState<string>("overview");
    const [removing, setRemoving] = React.useState(false);
    const [bulkStart, setBulkStart] = React.useState(false);
    const queryClient = useQueryClient();

    // Warmup is a paid/trial feature; gate the start controls when the org
    // isn't entitled. Treat unknown (still loading) as allowed — the backend
    // is the real enforcement point.
    const featureStatus = useFeatureStatus();
    const canWarmup = featureStatus.data?.can_use_warmup !== false;

    // One query feeds live health for every row; the realtime layer already
    // invalidates ["analytics","accounts",…] on warmup/account events.
    const statuses = useAccountStatuses();
    // Coerce to an array defensively: a wrong-shape (non-array) response must
    // never reach a `for…of`, which would throw "{} is not iterable".
    const accountStatuses = useMemo(
        () => (Array.isArray(statuses.data) ? statuses.data : []),
        [statuses.data],
    );
    const statusById = useMemo(() => {
        const m = new Map<string, AccountStatus>();
        for (const s of accountStatuses) m.set(s.id, s);
        return m;
    }, [accountStatuses]);

    // Proactively notify the user when a mailbox's health drops.
    const prevHealth = useRef<Map<string, string>>(new Map());
    useEffect(() => {
        if (accountStatuses.length === 0) return;
        const prev = prevHealth.current;
        const next = new Map<string, string>();
        for (const s of accountStatuses) {
            const cur = s.health?.status ?? "healthy";
            next.set(s.id, cur);
            const before = prev.get(s.id);
            if (before && (HEALTH_RANK[cur] ?? 0) > (HEALTH_RANK[before] ?? 0)) {
                const reason = s.warmup_health?.reason || s.health?.issues?.[0];
                toast.error(`${s.email} health dropped to ${cur}${reason ? ` — ${reason}` : ""}`);
            }
        }
        prevHealth.current = next;
    }, [accountStatuses]);

    const removeSelected = () => {
        if (selected.length === 0 || removing) return;
        const n = selected.length;
        confirm.show(
            `Remove ${n} mailbox${n > 1 ? "es" : ""}? This disconnects ${n > 1 ? "them" : "it"} from Warmbly.`,
            async () => {
                setRemoving(true);
                const results = await Promise.allSettled(selected.map((id) => removeEmail(id)));
                const failed = results.filter((r) => r.status === "rejected").length;
                await queryClient.invalidateQueries({ queryKey: ["emails"] });
                setSelected([]);
                setRemoving(false);
                if (failed > 0) toast.error(`${failed} mailbox${failed > 1 ? "es" : ""} couldn't be removed`);
                else toast.success(`Removed ${n} mailbox${n > 1 ? "es" : ""}`);
            },
        );
    };

    const bulkWarmup = async (action: "start" | "pause") => {
        if (selected.length === 0) return;
        const n = selected.length;
        const results = await Promise.allSettled(selected.map((id) => warmupLifecycle(id, action)));
        const failed = results.filter((r) => r.status === "rejected").length;
        await queryClient.invalidateQueries({ queryKey: ["emails", "list"] });
        await queryClient.invalidateQueries({ queryKey: ["analytics", "accounts"] });
        setSelected([]);
        const verb = action === "start" ? "started" : "paused";
        if (failed > 0) toast.error(`${failed} mailbox${failed > 1 ? "es" : ""} couldn't be updated`);
        else toast.success(`Warmup ${verb} for ${n} mailbox${n > 1 ? "es" : ""}`);
    };

    const openDetail = (id: string, tab: string = "overview") => {
        setViewTab(tab);
        setView(id);
    };

    const stag = useMemo(() => {
        if (!p) return DefaultFolder;
        const f = p.user.tags.find((t) => t.id === tag);
        if (!f) return DefaultFolder;
        return f;
    }, [tag, p]);

    const stats = useMemo(() => {
        if (!emailsData.emails) return { total: 0, healthy: 0, warming: 0, issues: 0 };
        return {
            total: emailsData.emails.length,
            healthy: emailsData.emails.filter((e) => e.status === "healthy").length,
            warming: emailsData.emails.filter((e) => e.status === "warming").length,
            issues: emailsData.emails.filter((e) => e.status !== "healthy" && e.status !== "warming").length,
        };
    }, [emailsData.emails]);

    function isSelectedAll(): boolean {
        return emailsData.emails
            ? emailsData.emails.length > 0 && selected.length === emailsData.emails.length
            : false;
    }

    return (
        <Page>
            <PageTopbar
                eyebrow="Accounts"
                subtitle={
                    emailsData.emails
                        ? `${stats.total} mailboxes`
                        : "Loading…"
                }
            >
                <TopbarAction
                    onClick={() => p?.setAddEmail(true)}
                    icon={<PlusIcon className="w-3 h-3" />}
                >
                    Add account
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Total" value={<AnimatedNumber value={stats.total} />} sub="connected" />
                <Stat label="Healthy" value={<AnimatedNumber value={stats.healthy} />} sub="sending now" accent={stats.healthy > 0} />
                <Stat label="Warming" value={<AnimatedNumber value={stats.warming} />} sub="ramping up" />
                <Stat label="Needs attention" value={<AnimatedNumber value={stats.issues} />} sub="paused or failing" last />
            </StatStrip>

            <SectionBar label="Mailboxes" count={emailsData.emails?.length ?? 0}>
                <SearchInput
                    value={query}
                    onChange={setQuery}
                    placeholder="Search by email…"
                    className="w-full sm:w-56"
                />
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <SelectButton
                            icon={<FilterIcon className="w-3.5 h-3.5" />}
                            label={stag.title}
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={200}>
                        <PopoverMenuLabel>Tags</PopoverMenuLabel>
                        <PopoverMenuItem
                            onSelect={() => setTag("")}
                            selected={!tag}
                        >
                            All accounts
                        </PopoverMenuItem>
                        {(p?.user.tags ?? []).map((t) => (
                            <PopoverMenuItem
                                key={t.id}
                                onSelect={() => setTag(tag === t.id ? "" : t.id)}
                                icon={<span className="size-2 rounded-full" style={{ backgroundColor: t.color }} />}
                                selected={tag === t.id}
                            >
                                {t.title}
                            </PopoverMenuItem>
                        ))}
                        <PopoverMenuSeparator />
                        <PopoverMenuItem
                            onSelect={() => p?.setTagsEdit(true)}
                            icon={<Settings2Icon className="w-3 h-3" />}
                        >
                            Manage tags
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </SectionBar>

            <PageBody>
                {emailsData.isLoading ? (
                    <div className="divide-y divide-slate-200/60">
                        {Array.from({ length: 6 }).map((_, i) => (
                            <div key={i} className="h-11 px-5 flex items-center gap-3">
                                <div className="w-3.5 h-3.5 bg-slate-100 rounded" />
                                <div className="w-6 h-6 rounded-full bg-slate-100 shrink-0" />
                                <div className="h-3 w-52 bg-slate-100 rounded animate-pulse" />
                                <div className="ml-auto h-3 w-16 bg-slate-100 rounded animate-pulse" />
                            </div>
                        ))}
                    </div>
                ) : !emailsData.emails || emailsData.emails.length === 0 ? (
                    <EmptyBlock
                        title="No email accounts yet"
                        body="Connect your first mailbox to start warming up and sending campaigns."
                        cta={
                            <TopbarAction
                                onClick={() => p?.setAddEmail(true)}
                                icon={<PlusIcon className="w-3 h-3" />}
                            >
                                Add account
                            </TopbarAction>
                        }
                    />
                ) : (
                    <table className="w-full text-left">
                        <thead className="sticky top-0 bg-white z-[1]">
                            <tr className="border-b border-slate-200">
                                <th className="pl-5 pr-2 py-2 w-9">
                                    <input
                                        type="checkbox"
                                        className="w-3.5 h-3.5 rounded accent-sky-600"
                                        checked={isSelectedAll()}
                                        onChange={() => {
                                            if (isSelectedAll()) {
                                                setSelected((bef) =>
                                                    bef.filter((e) => !emailsData.emails.map((em) => em.id).includes(e)),
                                                );
                                            } else {
                                                setSelected((bef) => [
                                                    ...bef,
                                                    ...emailsData.emails
                                                        .filter((em) => !selected.includes(em.id))
                                                        .map((em) => em.id),
                                                ]);
                                            }
                                        }}
                                    />
                                </th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Account</th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-24 text-right">Warmup</th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-32">Health</th>
                                <th className="px-3 py-2 w-16"></th>
                            </tr>
                        </thead>
                        <tbody>
                            {emailsData.emails.map((box) => (
                                <MailboxRow
                                    key={box.id}
                                    box={box}
                                    status={statusById.get(box.id)}
                                    canWarmup={canWarmup}
                                    checked={selected.includes(box.id)}
                                    onToggleSelect={() =>
                                        selected.includes(box.id)
                                            ? setSelected((bef) => bef.filter((i) => i !== box.id))
                                            : setSelected((bef) => [...bef, box.id])
                                    }
                                    onOpen={openDetail}
                                />
                            ))}
                        </tbody>
                    </table>
                )}

                {selected.length > 0 && (
                    <div className="fixed bottom-5 left-1/2 -translate-x-1/2 z-30 flex items-center gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5">
                        <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                            <CheckIcon className="w-3 h-3" />
                            <span>{selected.length} selected</span>
                        </div>
                        {canWarmup && (
                            <button
                                type="button"
                                onClick={() => setBulkStart(true)}
                                className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-orange-600 hover:bg-orange-50 transition-colors"
                            >
                                <PlayIcon className="w-3.5 h-3.5" />
                                Start warmup
                            </button>
                        )}
                        <button
                            type="button"
                            onClick={() => bulkWarmup("pause")}
                            className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-slate-600 hover:bg-slate-100 transition-colors"
                        >
                            <PauseIcon className="w-3.5 h-3.5" />
                            Pause
                        </button>
                        <div className="w-px h-4 bg-slate-200 mx-0.5" />
                        <button
                            type="button"
                            onClick={removeSelected}
                            disabled={removing}
                            className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-red-600 hover:bg-red-50 disabled:opacity-50 transition-colors"
                        >
                            <Trash2Icon className="w-3.5 h-3.5" />
                            Remove
                        </button>
                        <button
                            type="button"
                            onClick={() => setSelected([])}
                            className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] text-slate-500 hover:bg-slate-100 transition-colors"
                        >
                            <XIcon className="w-3.5 h-3.5" />
                            Clear
                        </button>
                    </div>
                )}
            </PageBody>

            <InboxDetails emails={emailsData.emails} view={view} setView={setView} initialTab={viewTab} canWarmup={canWarmup} />

            <BulkWarmupDialog
                open={bulkStart}
                ids={selected}
                onClose={() => setBulkStart(false)}
                onComplete={() => setSelected([])}
            />
        </Page>
    );
}

/* ── one mailbox row + its warmup dropdown ───────────────────────────── */

function MailboxRow({
    box,
    status,
    canWarmup,
    checked,
    onToggleSelect,
    onOpen,
}: {
    box: Inbox;
    status?: AccountStatus;
    canWarmup: boolean;
    checked: boolean;
    onToggleSelect: () => void;
    onOpen: (id: string, tab?: string) => void;
}) {
    const life = useWarmupLifecycle(box.id);
    const confirm = useConfirm();

    const off = !box.warmup;
    const paused = !!box.warmup && !!box.warmup_paused_at;
    const active = !!box.warmup && !box.warmup_paused_at;

    const tone = healthTone(status);
    const ws = status?.warmup_status;
    const inCampaign = status?.in_campaign;

    // Warmup column: what's flowing today and why.
    const warmupLabel = active
        ? `${ws?.current_volume ?? 0}/${ws?.target_volume ?? box.warmup_base}`
        : paused
            ? "Paused"
            : inCampaign
                ? "Health-check"
                : "Off";
    const warmupTone = active
        ? "text-orange-600"
        : paused
            ? "text-amber-600"
            : inCampaign
                ? "text-sky-600"
                : "text-slate-400";

    const run = (action: "start" | "pause" | "resume", verb: string) => {
        life.mutate(action, {
            onSuccess: () => toast.success(`Warmup ${verb} for ${box.email}`),
            onError: () => toast.error("Couldn't update warmup"),
        });
    };

    const stopReset = () => {
        confirm.show(
            `Stop warmup for ${box.email}? This resets ramp progress — restarting begins from the base volume. Use Pause to keep progress.`,
            async () => {
                try {
                    await life.mutateAsync("stop");
                    toast.success(`Warmup stopped for ${box.email}`);
                } catch {
                    toast.error("Couldn't update warmup");
                }
            },
        );
    };

    const upsell = () => toast("Warmup is available on paid plans", { icon: "✨" });

    return (
        <tr
            onClick={() => onOpen(box.id)}
            className="border-b border-slate-200/60 hover:bg-slate-50/80 transition-colors group h-11 cursor-pointer"
        >
            <td className="pl-5 pr-2">
                <input
                    type="checkbox"
                    className="w-3.5 h-3.5 rounded accent-sky-600"
                    checked={checked}
                    onChange={onToggleSelect}
                    onClick={(e) => e.stopPropagation()}
                />
            </td>
            <td className="px-3">
                <button type="button" onClick={(e) => { e.stopPropagation(); onOpen(box.id); }} className="flex items-center gap-2.5 text-left">
                    <div className="w-6 h-6 rounded-full bg-sky-100 flex items-center justify-center shrink-0">
                        <span className="text-[9.5px] font-semibold text-sky-700">
                            {box.email.slice(0, 2).toUpperCase()}
                        </span>
                    </div>
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{box.email}</span>
                    {inCampaign && (
                        <span className="hidden sm:inline-flex items-center gap-1 h-4 px-1.5 rounded-full bg-sky-50 text-sky-600 text-[9.5px] font-medium uppercase tracking-[0.08em]">
                            <ActivityIcon className="w-2.5 h-2.5" /> In campaign
                        </span>
                    )}
                </button>
            </td>
            <td className={`px-3 text-[12px] tabular-nums text-right font-mono ${warmupTone}`}>
                {active ? (
                    <span className="inline-flex items-center justify-end gap-1.5">
                        <span className="campaign-grid shrink-0" aria-hidden />
                        <span>
                            <AnimatedNumber value={ws?.current_volume ?? 0} />/
                            {ws?.target_volume ?? box.warmup_base}
                        </span>
                    </span>
                ) : (
                    warmupLabel
                )}
            </td>
            <td className="px-3">
                <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); onOpen(box.id, "overview"); }}
                    className={`inline-flex items-center gap-1.5 text-[11px] font-medium ${tone.text}`}
                    title="View mailbox health"
                >
                    <span className="relative flex w-1.5 h-1.5">
                        {tone.pulse && (
                            <span className={`absolute inline-flex h-full w-full rounded-full opacity-60 animate-ping ${tone.dot}`} />
                        )}
                        <span className={`relative inline-flex w-1.5 h-1.5 rounded-full ${tone.dot}`} />
                    </span>
                    <span className="uppercase tracking-[0.08em]">{tone.label}</span>
                </button>
            </td>
            <td className="px-3">
                <div className="flex items-center gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity">
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label="Warmup actions"
                                disabled={life.isPending}
                                className="w-6 h-6 flex items-center justify-center rounded hover:bg-slate-100 text-slate-400 hover:text-orange-600 transition-colors cursor-pointer disabled:opacity-50"
                            >
                                <RiFireLine className={`w-3.5 h-3.5 ${active ? "text-orange-500" : paused ? "text-amber-500" : ""}`} />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={208}>
                            <PopoverMenuLabel>Warmup · {active ? "Active" : paused ? "Paused" : "Off"}</PopoverMenuLabel>
                            {off && (
                                <PopoverMenuItem onSelect={canWarmup ? () => run("start", "started") : upsell} icon={<PlayIcon className="w-3 h-3" />}>
                                    {canWarmup ? "Start warmup" : "Upgrade to start warmup"}
                                </PopoverMenuItem>
                            )}
                            {paused && (
                                <PopoverMenuItem onSelect={canWarmup ? () => run("resume", "resumed") : upsell} icon={<PlayIcon className="w-3 h-3" />}>
                                    {canWarmup ? "Resume warmup" : "Upgrade to resume warmup"}
                                </PopoverMenuItem>
                            )}
                            {active && (
                                <PopoverMenuItem onSelect={() => run("pause", "paused")} icon={<PauseIcon className="w-3 h-3" />}>
                                    Pause warmup
                                </PopoverMenuItem>
                            )}
                            {(active || paused) && (
                                <PopoverMenuItem danger onSelect={stopReset} icon={<RotateCcwIcon className="w-3 h-3" />}>
                                    Stop &amp; reset
                                </PopoverMenuItem>
                            )}
                            <PopoverMenuSeparator />
                            <PopoverMenuItem onSelect={() => onOpen(box.id, "warmup")} icon={<RiFireLine className="w-3 h-3" />}>
                                Warmup settings
                            </PopoverMenuItem>
                            <PopoverMenuItem onSelect={() => onOpen(box.id, "overview")} icon={<GaugeIcon className="w-3 h-3" />}>
                                Mailbox health
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                    <button
                        type="button"
                        className="w-6 h-6 flex items-center justify-center rounded hover:bg-slate-100 text-slate-400 hover:text-slate-700 transition-colors cursor-pointer"
                        onClick={(e) => { e.stopPropagation(); onOpen(box.id, "settings"); }}
                        aria-label="Mailbox settings"
                    >
                        <RiMoreLine className="w-3.5 h-3.5" />
                    </button>
                </div>
            </td>
        </tr>
    );
}
