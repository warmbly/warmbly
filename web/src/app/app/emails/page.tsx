import { RiFireLine, RiMoreLine } from "@remixicon/react";
import React, { useEffect, useMemo, useRef } from "react";
import { useSearchParam, useSearchParams } from "@/hooks/useSearchParams";
import toast from "react-hot-toast/headless";
import { useQueryClient } from "@tanstack/react-query";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import useWarmupLifecycle from "@/lib/api/hooks/app/emails/useWarmupLifecycle";
import { diagnosticSendingAllowed, diagnosticWarmupActive } from "@/lib/diagnosticParticipation";
import useAccountStatuses from "@/lib/api/hooks/app/analytics/useAccountStatuses";
import useFeatureStatus from "@/lib/api/hooks/app/subscription/useFeatureStatus";
import warmupLifecycle from "@/lib/api/client/app/emails/warmupLifecycle";
import removeEmail from "@/lib/api/client/app/emails/removeEmail";
import updateEmail from "@/lib/api/client/app/emails/updateEmail";
import useMailboxSwitch, { switchOffPrompt } from "@/components/app/emails/useMailboxSwitch";
import invalidateAfterMailboxRemoval from "@/lib/api/hooks/app/emails/invalidateAfterMailboxRemoval";
import useRemoveEmail from "@/lib/api/hooks/app/emails/useRemoveEmail";
import { useUserProfile } from "@/hooks/context/user";
import { useConfirm } from "@/hooks/context/confirm";
import InboxDetails from "@/components/app/emails/InboxDetails";
import WarmupCoverageNotice from "@/components/app/emails/WarmupCoverageNotice";
import CloudPoolBanner from "@/components/app/emails/CloudPoolBanner";
import CloudPathsPanel from "@/components/app/emails/CloudPathsPanel";
import CloudConnectDialog from "@/components/app/cloud/CloudConnectDialog";
import useCloudPool from "@/hooks/useCloudPool";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { useEnrollCloudLinkMailbox, useUnenrollCloudLinkMailbox, useCloudLinkMailboxLifecycle } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import { providerSupported } from "@/app/app/settings/warmbly-cloud/providers";
import { CloudIcon, MailCheckIcon } from "lucide-react";
import { PlacementRateBadge } from "@/components/app/placement/PlacementCharts";
import type { CloudLinkMailboxRow } from "@/lib/api/models/app/cloudlink/CloudLink";
import { cloudSendFailure, cloudWarmupPaused } from "@/lib/cloudWarmup";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";
import BulkWarmupDialog from "@/components/app/emails/BulkWarmupDialog";
import BulkTagPopover from "@/components/app/emails/BulkTagPopover";
import MailboxImportsMenu from "@/components/app/emails/import/MailboxImportsMenu";
import MailboxSourceChip from "@/components/app/emails/MailboxSourceChip";
import ProviderLogo from "@/components/app/emails/ProviderLogo";
import SigninMigrationBanner, { SigninRetiringChip } from "@/components/app/emails/migration/SigninMigrationBanner";
import SigninMigrationDialog from "@/components/app/emails/migration/SigninMigrationDialog";
import MailboxGrantDialog from "@/components/app/emails/import/grants/MailboxGrantDialog";
import { useSigninMigration } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { mailboxBrand, mailboxSource } from "@/lib/mailboxSource";
import type Tag from "@/lib/api/models/app/Tag";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import mailboxDisplayStatus from "@/lib/mailboxStatus";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import {
    ActivityIcon,
    AlertTriangleIcon,
    ArrowDownIcon,
    ArrowUpIcon,
    CheckIcon,
    CircleSlashIcon,
    FilterIcon,
    FlameIcon,
    GaugeIcon,
    GlobeIcon,
    Loader2Icon,
    MoonIcon,
    PauseIcon,
    PlayIcon,
    PlusIcon,
    PowerIcon,
    PowerOffIcon,
    RotateCcwIcon,
    SendIcon,
    Settings2Icon,
    Trash2Icon,
    UnplugIcon,
    XIcon,
    type LucideIcon,
} from "lucide-react";
import { Dash, InfoHeader } from "@/components/app/contacts/cells";
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
    if (h.status === "healthy") return { dot: "bg-emerald-500", text: "text-emerald-700", label: `Healthy ${h.score}`, pulse: false };
    if (h.status === "warning") return { dot: "bg-amber-500", text: "text-amber-600", label: `At risk ${h.score}`, pulse: true };
    return { dot: "bg-rose-500", text: "text-rose-600", label: `Issue ${h.score}`, pulse: true };
}

import AdvisorRowFlag from "@/components/app/advisor/AdvisorRowFlag";
import AdvisorSummaryBar from "@/components/app/advisor/AdvisorSummaryBar";
import { useAdvisorEntityIndex } from "@/lib/api/hooks/app/advisor/useAdvisor";
import type { AdvisorFinding } from "@/lib/api/models/app/advisor/Advisor";
import { Checkbox } from "@/components/ui/checkbox";
import { labelInk } from "@/lib/utils";

export default function AddressesPage() {
    const p = useUserProfile();
    const confirm = useConfirm();
    const canView = usePermission("MANAGE_EMAILS");

    const [query, setQuery] = React.useState<string>("");
    const [selectedTag, setTag] = useSearchParam("tag");
    const tag = p.user.tags.some((t) => t.id === selectedTag) ? selectedTag : "";
    const emailsData = useEmails({ query, tag });
    const [selected, setSelected] = React.useState<string[]>([]);
    const [view, setView] = React.useState<string>("");
    const [viewTab, setViewTab] = React.useState<string>("overview");
    const [removing, setRemoving] = React.useState(false);
    const [bulkStart, setBulkStart] = React.useState(false);
    const [searchParams, setSearchParams] = useSearchParams();
    const queryClient = useQueryClient();

    // Mailboxes on the retiring per-mailbox Google sign-in, and where each one's domain moves.
    const migration = useSigninMigration(canView);
    const retiringDomain = useMemo(() => {
        const m = new Map<string, string>();
        for (const g of migration.data?.data ?? []) for (const b of g.mailboxes) m.set(b.id, g.domain);
        return m;
    }, [migration.data]);
    const [migrationOpen, setMigrationOpen] = React.useState(false);
    const [migrationFocus, setMigrationFocus] = React.useState<string | null>(null);
    const [setupDomain, setSetupDomain] = React.useState<string | null>(null);
    const openMigration = (domain: string | null = null) => {
        setMigrationFocus(domain);
        setMigrationOpen(true);
    };

    // Warmup is a paid/trial feature; gate the start controls when the org
    // isn't entitled. Treat unknown (still loading) as allowed — the backend
    // is the real enforcement point.
    const featureStatus = useFeatureStatus();
    const canWarmup = featureStatus.data?.can_use_warmup !== false;

    // Self-hosted instances can hand warmup to the Warmbly pool; the banner,
    // row badges and menu items below key off this.
    const cloud = useCloudPool();
    const [cloudDialog, setCloudDialog] = React.useState(false);
    const authConfigLoading = useAuthConfig().isLoading;

    // One query for the whole surface; each row reads its own advice out of the
    // index rather than asking for it.
    const advisor = useAdvisorEntityIndex("emails");

    // Live health for every loaded row, asked for by the visible mailbox ids
    // (in bounded chunks) rather than an inventory-wide walk. The realtime layer
    // already invalidates ["analytics","accounts",…] on warmup/account events.
    const visibleEmailIds = useMemo(
        () => (emailsData.emails ?? []).map((e) => e.id),
        [emailsData.emails],
    );
    const statuses = useAccountStatuses(visibleEmailIds);
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
        // Say what it does, because none of it comes back. The revocation
        // sentence is built from the providers actually selected: claiming
        // "access is revoked at the provider" over a selection containing an
        // Outlook mailbox would be untrue for that one, and Microsoft publishes
        // no way for us to remove a single app.
        const providers = new Set(
            selected.map((id) => emailsData.emails?.find((e) => e.id === id)?.provider).filter(Boolean) as string[],
        );
        confirm.show(
            `Remove ${n} mailbox${n > 1 ? "es" : ""}? This deletes ${n > 1 ? "their" : "its"} imported mail and warmup history. ${bulkRevocationNote(providers)} It cannot be undone — switch ${n > 1 ? "them" : "it"} off instead to just stop sending.`,
            async () => {
                setRemoving(true);
                const results = await Promise.allSettled(selected.map((id) => removeEmail(id)));
                const failed = results.filter((r) => r.status === "rejected");
                await invalidateAfterMailboxRemoval(queryClient);
                setSelected([]);
                setRemoving(false);
                if (failed.length > 0) {
                    // Surface the server's reason when there is one to show: a
                    // disconnect that could not reach the machine syncing the
                    // mailbox is worth retrying, and "couldn't be removed"
                    // alone does not say so.
                    const reason = failed.length === 1 ? removeErrorMessage(failed[0].reason) : undefined;
                    toast.error(reason ?? `${failed.length} mailbox${failed.length > 1 ? "es" : ""} couldn't be removed`);
                } else toast.success(`Removed ${n} mailbox${n > 1 ? "es" : ""}`);
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
        const seeds = results.filter(
            (r) => r.status === "rejected" && (r.reason as AppError | null)?.code === "mailbox_is_seed",
        ).length;
        if (failed > 0 && seeds === failed) {
            toast.error(`${seeds} mailbox${seeds > 1 ? "es are placement seed inboxes" : " is a placement seed inbox"}, and seeds never warm up.`);
        } else if (failed > 0) toast.error(`${failed} mailbox${failed > 1 ? "es" : ""} couldn't be updated`);
        else toast.success(`Warmup ${verb} for ${n} mailbox${n > 1 ? "es" : ""}`);
    };

    const boxStatusById = useMemo(() => {
        const m = new Map<string, string>();
        for (const e of emailsData.emails ?? []) m.set(e.id, e.status);
        return m;
    }, [emailsData.emails]);

    // The whole mailbox on or off; warmup and the campaign hold are separate switches.
    // Only rows whose state is known are switched; the rest stay selected.
    const [switching, setSwitching] = React.useState(false);
    const bulkSwitch = (on: boolean) => {
        if (switching) return;
        const ids = selected.filter((id) => boxStatusById.get(id) === (on ? "inactive" : "active"));
        if (ids.length === 0) return;
        const n = ids.length;
        const apply = async () => {
            setSwitching(true);
            try {
                const results = await Promise.allSettled(ids.map((id) => updateEmail(id, { status: on ? "active" : "inactive" })));
                const failed = results.filter((r) => r.status === "rejected").length;
                await queryClient.invalidateQueries({ queryKey: ["emails", "list"] });
                await queryClient.invalidateQueries({ queryKey: ["analytics", "accounts"] });
                setSelected((prev) => prev.filter((id) => !ids.includes(id)));
                if (failed > 0) toast.error(`${failed} mailbox${failed > 1 ? "es" : ""} couldn't be switched ${on ? "on" : "off"}`);
                else toast.success(`${n} mailbox${n > 1 ? "es" : ""} switched ${on ? "back on" : "off"}`);
            } finally {
                setSwitching(false);
            }
        };
        if (on) void apply();
        else confirm.show(switchOffPrompt(n > 1 ? `${n} mailboxes` : "this mailbox"), apply);
    };
    const selectedStatuses = useMemo(() => {
        const out = { on: 0, off: 0 };
        for (const id of selected) {
            const st = boxStatusById.get(id);
            if (st === "active") out.on++;
            else if (st === "inactive") out.off++;
        }
        return out;
    }, [selected, boxStatusById]);

    const openDetail = (id: string, tab: string = "overview") => {
        setViewTab(tab);
        setView(id);
    };

    // ?mailbox=<id> opens that mailbox's detail on arrival, so advisor advice
    // about one mailbox can link straight to it instead of dropping the reader
    // at the top of a list of twenty.
    useEffect(() => {
        const id = searchParams.get("mailbox");
        if (!id) return;
        openDetail(id, searchParams.get("tab") ?? "overview");
        // Consume it, or every later close would be undone by a re-render.
        setSearchParams(
            (prev) => {
                const next = new URLSearchParams(prev);
                next.delete("mailbox");
                next.delete("tab");
                return next;
            },
            { replace: true },
        );
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [searchParams]);

    const stag = useMemo(() => {
        if (!p) return DefaultFolder;
        const f = p.user.tags.find((t) => t.id === tag);
        if (!f) return DefaultFolder;
        return f;
    }, [tag, p]);

    const stats = useMemo(() => {
        const s = { total: 0, healthy: 0, warming: 0, issues: 0 };
        for (const e of emailsData.emails ?? []) {
            s.total++;
            const st = mailboxDisplayStatus(e);
            if (st === "healthy") s.healthy++;
            else if (st === "warming") s.warming++;
            else s.issues++;
        }
        return s;
    }, [emailsData.emails]);

    // Mailboxes actively warming (enabled and not paused). Warmup pairs mailboxes
    // with each other, so too few starves it; the notice below warns on that.
    const warmupActive = useMemo(
        () => (emailsData.emails ?? []).filter(diagnosticWarmupActive).length,
        [emailsData.emails],
    );

    function isSelectedAll(): boolean {
        return emailsData.emails
            ? emailsData.emails.length > 0 && selected.length === emailsData.emails.length
            : false;
    }

    // Every mailbox page is loaded, so the headers sort in the browser.
    const [sort, setSort] = React.useState<MailboxSort | null>(null);
    const sortBy = (col: MailboxColumn) =>
        setSort((cur) =>
            cur?.by === col.id ? { by: col.id, reverse: !cur.reverse } : { by: col.id, reverse: !!col.sortAsc },
        );
    const sortedEmails = useMemo(() => {
        const list = emailsData.emails ?? [];
        const col = sort && MAILBOX_COLUMNS.find((c) => c.id === sort.by);
        if (!sort || !col?.sortValue) return list;
        const value = col.sortValue;
        const dir = sort.reverse ? 1 : -1;
        return [...list].sort((a, b) => {
            const x = value(a, statusById.get(a.id));
            const y = value(b, statusById.get(b.id));
            if (x === y) return 0;
            return (x > y ? 1 : -1) * dir;
        });
    }, [emailsData.emails, sort, statusById]);

    if (!canView) {
        return <NoAccess feature="email accounts" permissionLabel="Manage mailboxes" />;
    }

    return (
        <Page>
            <PageTopbar
                eyebrow="Accounts"
                subtitle={
                    emailsData.isPending
                        ? "Loading…"
                        : `${stats.total.toLocaleString()}${emailsData.isLoadingRest || emailsData.isIncomplete ? "+" : ""} mailbox${stats.total === 1 ? "" : "es"}`
                }
            >
                <MailboxImportsMenu />
                <TopbarAction variant="ghost" href="/app/emails/domains" icon={<GlobeIcon className="w-3 h-3" />}>
                    Sending domains
                </TopbarAction>
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
                {/* One stack with one gap, so the bar and the strips below it
                    sit evenly and the whole block collapses when all are empty. */}
                <div className="px-5 py-3 flex flex-col gap-2 empty:hidden">
                    <AdvisorSummaryBar surface="emails" noun="mailbox" nounPlural="mailboxes" />
                    <SigninMigrationBanner total={migration.data?.total ?? 0} onOpen={() => openMigration()} />
                    <CloudPoolBanner onConnect={() => setCloudDialog(true)} mailboxCount={stats.total} />
                    {!emailsData.isLoading && <CloudPathsPanel mailboxCount={stats.total} onAdd={() => p?.setAddEmail(true)} />}
                    {/* Hosted, the pool is thousands of mailboxes: the pool-size advice is self-host only. */}
                    {cloud.selfHosted && (
                        <WarmupCoverageNotice
                            warmupCount={warmupActive}
                            totalCount={stats.total}
                            canWarmup={canWarmup}
                            onAdd={() => p?.setAddEmail(true)}
                            onConnectCloud={cloud.manageable && !cloud.connected ? () => setCloudDialog(true) : undefined}
                            cloudConnected={cloud.workspaceConnected}
                        />
                    )}
                </div>
                <CloudConnectDialog open={cloudDialog} onClose={() => setCloudDialog(false)} />
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
                    cloud.selfHosted || authConfigLoading ? (
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
                    ) : null
                ) : (
                    // table-fixed like the Leads list: every column but Mailbox
                    // carries a width, so one long address never widens the table.
                    <table className="w-full table-fixed text-left">
                        <thead className="sticky top-0 bg-white z-[1]">
                            <tr className="border-b border-slate-200">
                                <th className="pl-5 pr-2 py-2 w-11">
                                    <Checkbox
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
                                {MAILBOX_COLUMNS.map((col) => (
                                    <MailboxTh key={col.id} col={col} sort={sort} onSort={sortBy} />
                                ))}
                                <th className="px-3 py-2 w-[76px]"></th>
                            </tr>
                        </thead>
                        <tbody>
                            {sortedEmails.map((box) => (
                                <MailboxRow
                                    key={box.id}
                                    box={box}
                                    tags={p?.user.tags ?? []}
                                    status={statusById.get(box.id)}
                                    findings={advisor.get(box.id)}
                                    canWarmup={canWarmup}
                                    cloud={cloud.connected ? cloud.rowFor(box.id) : undefined}
                                    cloudConnected={cloud.workspaceConnected}
                                    retiring={retiringDomain.has(box.id)}
                                    onRetiring={() => openMigration(retiringDomain.get(box.id) ?? null)}
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
                {emailsData.isLoadingRest && !emailsData.isPending && (
                    <div className="h-11 px-5 flex items-center gap-2 text-[12px] text-slate-400 border-b border-slate-200/60">
                        <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                        Loading more mailboxes…
                    </div>
                )}
                {emailsData.isIncomplete && (
                    <div className="h-11 px-5 flex items-center gap-2 text-[12px] text-amber-700 bg-amber-50/60 border-b border-amber-200/60">
                        <span>Showing {stats.total.toLocaleString()} mailboxes; the rest couldn't be loaded.</span>
                        <button
                            type="button"
                            onClick={() => void emailsData.fetchNextPage()}
                            className="ml-auto inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-amber-800 hover:bg-amber-100 transition-colors"
                        >
                            <RotateCcwIcon className="w-3.5 h-3.5" />
                            Retry
                        </button>
                    </div>
                )}

                {selected.length > 0 && (
                    <div className="fixed bottom-[max(1.25rem,env(safe-area-inset-bottom))] left-1/2 -translate-x-1/2 z-30 flex flex-wrap justify-center max-w-[calc(100vw-1rem)] items-center gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5">
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
                            Pause warmup
                        </button>
                        {selectedStatuses.off > 0 && (
                            <button
                                type="button"
                                onClick={() => bulkSwitch(true)}
                                disabled={switching}
                                className="disabled:opacity-50 inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-sky-700 hover:bg-sky-50 transition-colors"
                            >
                                <PowerIcon className="w-3.5 h-3.5" />
                                Switch on
                            </button>
                        )}
                        {selectedStatuses.on > 0 && (
                            <button
                                type="button"
                                onClick={() => bulkSwitch(false)}
                                disabled={switching}
                                className="disabled:opacity-50 inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-slate-600 hover:bg-slate-100 transition-colors"
                            >
                                <PowerOffIcon className="w-3.5 h-3.5" />
                                Switch off
                            </button>
                        )}
                        <BulkTagPopover ids={selected} />
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

            <SigninMigrationDialog
                open={migrationOpen}
                focusDomain={migrationFocus}
                onClose={() => setMigrationOpen(false)}
                onSetUpDomain={(domain) => {
                    setMigrationOpen(false);
                    setSetupDomain(domain);
                }}
            />
            <MailboxGrantDialog
                open={!!setupDomain}
                provider="google"
                initialDomain={setupDomain ?? undefined}
                onClose={() => setSetupDomain(null)}
            />

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

// revocationNote says what disconnecting does to the connection itself, which
// is not the same question for every provider. Google accepts a revocation and
// the app disappears from the customer's account; Microsoft publishes no
// endpoint for removing a single application, so all we can truthfully claim
// there is that our copy of the tokens is destroyed.
function revocationNote(provider?: string): string {
    if (provider === "gmail") return "Warmbly's access to the Google account is revoked.";
    if (provider === "outlook")
        return "The stored Microsoft tokens are destroyed; remove Warmbly itself from your Microsoft account privacy settings.";
    return "The stored credentials are destroyed.";
}

// bulkRevocationNote is the same answer for a mixed selection, which must not
// round up to the stronger claim.
function bulkRevocationNote(providers: Set<string>): string {
    const gmail = providers.has("gmail");
    const outlook = providers.has("outlook");
    if (providers.size === 1 && (gmail || outlook)) return revocationNote(gmail ? "gmail" : "outlook");
    if (gmail && outlook)
        return "Google access is revoked; the Microsoft tokens are destroyed here, and Warmbly is removed from a Microsoft account by you.";
    if (outlook)
        return "The stored credentials are destroyed; remove Warmbly itself from your Microsoft account privacy settings.";
    if (gmail) return "The stored credentials are destroyed, and Google access is revoked.";
    // No OAuth mailbox in the selection, so there is no grant to mention.
    return "The stored credentials are destroyed.";
}

// removeErrorMessage pulls the API's own explanation out of a failed request.
function removeErrorMessage(err: unknown): string | undefined {
    const e = err as { response?: { data?: { message?: string } } };
    return e?.response?.data?.message;
}

function MailboxRow({
    box,
    tags,
    status,
    findings,
    canWarmup,
    cloud,
    cloudConnected,
    retiring,
    onRetiring,
    checked,
    onToggleSelect,
    onOpen,
}: {
    box: Inbox;
    tags: Tag[];
    status?: AccountStatus;
    findings: AdvisorFinding[];
    canWarmup: boolean;
    cloud?: CloudLinkMailboxRow;
    cloudConnected: boolean;
    /** On the retiring per-mailbox Google sign-in. */
    retiring: boolean;
    onRetiring: () => void;
    checked: boolean;
    onToggleSelect: () => void;
    onOpen: (id: string, tab?: string) => void;
}) {
    const life = useWarmupLifecycle(box.id);
    const confirm = useConfirm();
    const remove = useRemoveEmail(box.id);
    const power = useMailboxSwitch(box.id, box.email);
    // Switched off by its owner or the platform; a revoked one needs a reconnect instead.
    const switchedOff = box.status === "inactive";

    // Disconnecting is unrecoverable and takes the mailbox's stored mail with
    // it, so the prompt says that rather than "are you sure". What happens to
    // the connection differs by provider and the copy has to differ with it:
    // Google accepts a revocation, Microsoft publishes no way for us to remove
    // one app, so promising it for Outlook would be a promise we cannot keep.
    const askDisconnect = () =>
        confirm.show(
            `Disconnect ${box.email}? This deletes its imported mail, warmup history and credentials. ${revocationNote(box.provider)} It cannot be undone — switch the mailbox off instead to just stop sending.`,
            async () => {
                try {
                    await remove.mutateAsync();
                    toast.success(`${box.email} disconnected`);
                } catch (e) {
                    toast.error(removeErrorMessage(e) ?? "The mailbox couldn't be disconnected");
                }
            },
        );

    // Resolve the row's tag ids against the user's tag registry; cap the chips
    // so long tag lists don't crowd the email out of the cell.
    const rowTags = useMemo(
        () =>
            (box.tags ?? [])
                .map((id) => tags.find((t) => t.id === id))
                .filter((t): t is Tag => !!t),
        [box.tags, tags],
    );
    const shownTags = rowTags.slice(0, 3);
    const source = mailboxSource(box);
    const brand = mailboxBrand(box);

    const off = !box.warmup || box.test_mode === "off";
    const paused = !off && !!box.warmup && (!!box.warmup_paused_at || !diagnosticSendingAllowed(box));
    const active = diagnosticWarmupActive(box);
    // Warmup only runs on a mailbox that is on, whatever its warmup setting says.
    const warming = active && box.status === "active";

    const tone = healthTone(status);
    const ws = status?.warmup_status;
    const inCampaign = status?.in_campaign;

    const cloudEnroll = useEnrollCloudLinkMailbox();
    const cloudUnenroll = useUnenrollCloudLinkMailbox();
    const cloudLifecycle = useCloudLinkMailboxLifecycle();
    const inCloud = !!cloud?.enrolled;
    const cloudPaused = cloudWarmupPaused(cloud?.cloud);
    const cloudSupported = providerSupported(box.provider);
    const cloudRun = async (fn: () => Promise<unknown>, ok: string) => {
        try {
            await fn();
            toast.success(ok);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    // A refused send outranks the count: it is why the count is not moving.
    const sendFailure = inCloud ? cloudSendFailure(cloud?.cloud) : warming ? ws?.send_failure : undefined;

    // Warmup column: what's flowing today and why.
    const warmupLabel = inCloud
        ? "Cloud"
        : warming
        ? `${ws?.current_volume ?? 0}/${ws?.target_volume ?? box.warmup_base}`
        : active
            ? "Stopped"
        : paused
            ? "Paused"
            : inCampaign && diagnosticSendingAllowed(box)
                ? "Health-check"
                : "Off";
    const warmupTone = sendFailure
        ? "text-rose-600"
        : inCloud
        ? cloudPaused
            ? "text-amber-600"
            : cloud?.cloud
              ? "text-orange-600"
              : "text-sky-600"
        : warming
        ? "text-orange-600"
        : active
            ? "text-slate-400"
        : paused
            ? "text-amber-600"
            : inCampaign && diagnosticSendingAllowed(box)
                ? "text-sky-600"
                : "text-slate-400";

    const run = (action: "start" | "pause" | "resume", verb: string) => {
        const execute = () => life.mutate(action, {
            onSuccess: () => toast.success(`Warmup ${verb} for ${box.email}`),
            onError: (e) => toast.error(warmupErrorMessage(e as unknown as AppError)),
        });
        if (action === "start" || action === "resume") {
            confirm.show("Authorize disclosed automated diagnostic sending? Starting from Off also enables receiving tests. This is not organic engagement or a proven reputation benefit; provider policies apply.", execute);
        } else execute();
    };

    const stopReset = () => {
        confirm.show(
            `Stop all diagnostic sending and receiving for ${box.email}? Ramp history and safety holds are retained. Updated workers recheck pending work, but accepted provider sends cannot be recalled.`,
            async () => {
                try {
                    await life.mutateAsync("stop");
                    toast.success(`Warmup stopped for ${box.email}`);
                } catch (e) {
                    toast.error(warmupErrorMessage(e as AppError));
                }
            },
        );
    };

    const upsell = () => toast("Warmup is available on paid plans", { icon: "✨" });

    return (
        <tr
            onClick={() => onOpen(box.id)}
            className={`border-b border-slate-200/60 transition-colors group h-11 cursor-pointer ${checked ? "bg-sky-50/60" : "hover:bg-slate-50/80"}`}
        >
            <td className="pl-5 pr-2" onClick={(e) => e.stopPropagation()}>
                <Checkbox checked={checked} onChange={onToggleSelect} />
            </td>
            <td className="px-3 overflow-hidden">
                {/* The flag is a sibling of the open-row button, not a child:
                    it has its own trigger and nesting buttons is invalid. */}
                <div className="flex w-full min-w-0 items-center gap-2">
                <button type="button" onClick={(e) => { e.stopPropagation(); onOpen(box.id); }} className="flex min-w-0 flex-1 items-center gap-2.5 text-left">
                    <span className="relative shrink-0">
                        <ProviderLogo id={brand} size="md" title={source.title || source.label} className="rounded-full" />
                        {inCloud && (
                            <span
                                aria-hidden
                                className={`absolute -bottom-1 -right-1 size-3.5 rounded-full ring-2 ring-white inline-flex items-center justify-center text-white ${cloudPaused ? "bg-amber-500" : "bg-sky-600"}`}
                            >
                                <CloudIcon className="w-2 h-2" strokeWidth={3} />
                            </span>
                        )}
                    </span>
                    <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-1.5 min-w-0 leading-tight">
                            <span className="text-[12.5px] font-medium text-slate-900 truncate">{box.email}</span>
                            {inCloud && (
                                <span
                                    title={cloud?.managed ? "Signed in through Warmbly Cloud, which warms it" : cloudPaused ? "Paused in Warmbly Cloud" : "Warmed by Warmbly Cloud"}
                                    className={`inline-flex items-center gap-1 h-4 px-1.5 rounded-full text-[9.5px] font-medium uppercase tracking-[0.08em] shrink-0 ${cloudPaused ? "bg-amber-50 text-amber-600" : "bg-sky-600 text-white"}`}
                                >
                                    <CloudIcon className="w-2.5 h-2.5" /> Cloud
                                </span>
                            )}
                            {shownTags.map((t) => (
                                <span
                                    key={t.id}
                                    className="hidden lg:inline-flex items-center gap-1 h-4 px-1.5 rounded-full text-[9.5px] font-medium shrink-0"
                                    style={{ backgroundColor: `${t.color}1a`, color: labelInk(t.color) }}
                                >
                                    <span className="size-1.5 rounded-full" style={{ backgroundColor: t.color }} />
                                    {t.title}
                                </span>
                            ))}
                            {rowTags.length > shownTags.length && (
                                <span className="hidden lg:inline-flex items-center h-4 px-1 rounded-full bg-slate-100 text-slate-500 text-[9.5px] font-medium shrink-0">
                                    +{rowTags.length - shownTags.length}
                                </span>
                            )}
                        </div>
                        {/* Who it sends as, and how it connects: the vendor or grant chip, else the host. */}
                        <div className="mt-0.5 flex items-center gap-1.5 min-w-0 text-[11px] text-slate-400 leading-tight">
                            {box.name && <span className="truncate text-slate-500">{box.name}</span>}
                            {box.name && <span className="text-slate-300">·</span>}
                            {source.kind !== "host" ? (
                                <MailboxSourceChip box={box} labelClassName="inline" />
                            ) : (
                                <span className="truncate" title={source.title}>{source.label}</span>
                            )}
                            {inCloud && (
                                <span className={`inline-flex items-center gap-1 shrink-0 ${cloudPaused ? "text-amber-600" : "text-sky-600"}`}>
                                    <span className="text-slate-300">·</span>
                                    <CloudIcon className="w-2.5 h-2.5" /> {cloudPaused ? "Cloud warmup paused" : "Warmed by Warmbly Cloud"}
                                </span>
                            )}
                            {inCampaign && (
                                <span className="hidden sm:inline-flex items-center gap-1 shrink-0 text-sky-600">
                                    <span className="text-slate-300">·</span>
                                    <ActivityIcon className="w-2.5 h-2.5" /> In campaign
                                </span>
                            )}
                        </div>
                    </div>
                </button>
                {retiring && <SigninRetiringChip onClick={onRetiring} />}
                <AdvisorRowFlag findings={findings} subject={box.email} />
                </div>
            </td>
            <td className="px-3 overflow-hidden">
                <MailboxStatusPill box={box} status={status} warming={inCloud ? !cloudPaused : warming} />
            </td>
            <td className="px-3 overflow-hidden hidden md:table-cell">
                {status?.daily_usage ? (
                    <span
                        className={`inline-flex items-center gap-1.5 font-mono text-[12px] tabular-nums ${status.daily_usage.campaign_sent > 0 ? "text-sky-700" : "text-slate-500"}`}
                        title={`${status.daily_usage.campaign_sent} of ${status.daily_usage.campaign_limit || box.campaign_limit} campaign emails sent today`}
                    >
                        <SendIcon className="w-3 h-3 shrink-0 text-sky-500" />
                        <span>
                            <AnimatedNumber value={status.daily_usage.campaign_sent} />
                            <span className="text-slate-400">/{status.daily_usage.campaign_limit || box.campaign_limit}</span>
                        </span>
                    </span>
                ) : (
                    <Dash />
                )}
            </td>
            <td className={`px-3 overflow-hidden font-mono text-[12px] tabular-nums ${warmupTone}`}>
                {inCloud && cloud?.cloud ? (
                    <span
                        className="inline-flex items-center gap-1.5"
                        title={
                            sendFailure
                                ? `Warmbly Cloud could not send from this mailbox: ${sendFailure.message}`
                                : `${cloudPaused ? "Paused in Warmbly Cloud. " : ""}${cloud.cloud.sent_today} of ${cloud.cloud.warmup?.target_volume ?? cloud.cloud.settings.base} warmup emails sent today by Warmbly Cloud`
                        }
                    >
                        {sendFailure ? (
                            <AlertTriangleIcon className="w-3 h-3 shrink-0" />
                        ) : cloudPaused ? (
                            <PauseIcon className="w-3 h-3 shrink-0" />
                        ) : (
                            <span className="campaign-grid shrink-0" aria-hidden />
                        )}
                        <span>
                            <AnimatedNumber value={cloud.cloud.sent_today} />
                            <span className="opacity-60">/{cloud.cloud.warmup?.target_volume ?? cloud.cloud.settings.base}</span>
                        </span>
                    </span>
                ) : inCloud ? (
                    <span className="inline-flex items-center gap-1.5" title="Waiting for Warmbly Cloud to report">
                        <CloudIcon className="w-3 h-3 shrink-0" />
                        <span>{warmupLabel}</span>
                    </span>
                ) : warming ? (
                    <span
                        className="inline-flex items-center gap-1.5"
                        title={sendFailure ? `The last warmup email was not sent: ${sendFailure.message}` : `${ws?.current_volume ?? 0} of ${ws?.target_volume ?? box.warmup_base} warmup emails sent today`}
                    >
                        {sendFailure ? <AlertTriangleIcon className="w-3 h-3 shrink-0" /> : <span className="campaign-grid shrink-0" aria-hidden />}
                        <span>
                            <AnimatedNumber value={ws?.current_volume ?? 0} />
                            <span className="opacity-60">/{ws?.target_volume ?? box.warmup_base}</span>
                        </span>
                    </span>
                ) : (
                    <span
                        className="inline-flex items-center gap-1.5 font-sans text-[11.5px] font-medium"
                        title={
                            active
                                ? box.status === "revoked"
                                    ? "Warmup is on, but the mailbox's access was revoked, so nothing is sent. Reconnect it to resume."
                                    : "Warmup is on, but the mailbox is not, so nothing is sent. Switch the mailbox back on to resume."
                                : undefined
                        }
                    >
                        {active ? (
                            <PowerOffIcon className="w-3 h-3 shrink-0" />
                        ) : paused ? (
                            <PauseIcon className="w-3 h-3 shrink-0" />
                        ) : inCampaign ? (
                            <ActivityIcon className="w-3 h-3 shrink-0" />
                        ) : (
                            <RiFireLine className="w-3 h-3 shrink-0" />
                        )}
                        {warmupLabel}
                    </span>
                )}
            </td>
            <td className="px-3">
                <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); onOpen(box.id, "deliverability"); }}
                    aria-label="View warmup deliverability"
                    className="inline-flex items-center gap-1.5"
                >
                    <MailCheckIcon className="w-3 h-3 shrink-0 text-slate-400 hidden sm:block" />
                    <PlacementRateBadge rate={status?.warmup_placement} />
                </button>
            </td>
            <td className="px-3 overflow-hidden">
                <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); onOpen(box.id, "overview"); }}
                    className={`inline-flex items-center gap-1.5 text-[11px] font-medium max-w-full ${tone.text}`}
                    title={status?.health?.issues?.join("\n") || "View mailbox health"}
                >
                    <span className={`inline-flex w-1.5 h-1.5 rounded-full shrink-0 ${tone.dot}`} />
                    <span className={`uppercase tracking-[0.08em] hidden md:inline truncate ${tone.pulse ? "text-shimmer" : ""}`}>{tone.label}</span>
                </button>
            </td>
            <td className="px-3" onClick={(e) => e.stopPropagation()}>
                <div className="flex items-center gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity">
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label="Warmup actions"
                                disabled={life.isPending}
                                className="w-6 h-6 flex items-center justify-center rounded hover:bg-slate-100 text-slate-400 hover:text-orange-600 transition-colors cursor-pointer disabled:opacity-50"
                            >
                                {inCloud ? <CloudIcon className={`w-3.5 h-3.5 ${cloudPaused ? "text-amber-500" : "text-sky-600"}`} /> : <RiFireLine className={`w-3.5 h-3.5 ${warming ? "text-orange-500" : paused ? "text-amber-500" : ""}`} />}
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={208}>
                            <PopoverMenuLabel>Warmup · {inCloud ? (cloudPaused ? "Paused in cloud" : "Warmbly Cloud") : box.status === "revoked" && !off ? "Needs reconnect" : switchedOff && !off ? "Mailbox off" : active ? "Active" : paused ? "Paused" : "Off"}</PopoverMenuLabel>
                            {switchedOff && (
                                <>
                                    <PopoverMenuItem onSelect={power.switchOn} icon={<PowerIcon className="w-3 h-3" />}>
                                        Switch the mailbox back on
                                    </PopoverMenuItem>
                                    <PopoverMenuSeparator />
                                </>
                            )}
                            {inCloud && (
                                <>
                                    <PopoverMenuItem
                                        onSelect={() => void cloudRun(() => cloudLifecycle.mutateAsync({ id: box.id, action: cloudPaused ? "resume" : "pause" }), cloudPaused ? "Warmup resumed" : "Warmup paused")}
                                        icon={cloudPaused ? <PlayIcon className="w-3 h-3" /> : <PauseIcon className="w-3 h-3" />}
                                    >
                                        {cloudPaused ? "Resume in Warmbly Cloud" : "Pause in Warmbly Cloud"}
                                    </PopoverMenuItem>
                                    <PopoverMenuItem
                                        danger
                                        onSelect={() =>
                                            confirm.show(
                                                cloud?.managed
                                                    ? `Remove ${box.email} from this instance? It stays in your Warmbly Cloud workspace, where its sign-in lives; campaigns here stop sending from it.`
                                                    : `Stop warming ${box.email} in the Warmbly pool? The cloud deletes its credential right away.`,
                                                async () => {
                                                    await cloudRun(() => cloudUnenroll.mutateAsync(box.id), cloud?.managed ? `${box.email} removed from this instance` : `${box.email} removed from the pool`);
                                                },
                                            )
                                        }
                                        icon={<CloudIcon className="w-3 h-3" />}
                                    >
                                        {cloud?.managed ? "Remove from this instance" : "Remove from Warmbly Cloud"}
                                    </PopoverMenuItem>
                                    <PopoverMenuSeparator />
                                </>
                            )}
                            {!inCloud && cloudConnected && cloudSupported && (
                                <PopoverMenuItem
                                    onSelect={() => void cloudRun(() => cloudEnroll.mutateAsync(box.id), `${box.email} is now warming in the pool`)}
                                    icon={<CloudIcon className="w-3 h-3" />}
                                >
                                    Warm in Warmbly Cloud
                                </PopoverMenuItem>
                            )}
                            {!inCloud && off && (
                                <PopoverMenuItem onSelect={canWarmup ? () => run("start", "started") : upsell} icon={<PlayIcon className="w-3 h-3" />}>
                                    {canWarmup ? "Start warmup" : "Upgrade to start warmup"}
                                </PopoverMenuItem>
                            )}
                            {!inCloud && paused && (
                                <PopoverMenuItem onSelect={canWarmup ? () => run("resume", "resumed") : upsell} icon={<PlayIcon className="w-3 h-3" />}>
                                    {canWarmup ? "Resume warmup" : "Upgrade to resume warmup"}
                                </PopoverMenuItem>
                            )}
                            {!inCloud && active && (
                                <PopoverMenuItem onSelect={() => run("pause", "paused")} icon={<PauseIcon className="w-3 h-3" />}>
                                    Pause warmup
                                </PopoverMenuItem>
                            )}
                            {!inCloud && (active || paused) && (
                                <PopoverMenuItem danger onSelect={stopReset} icon={<RotateCcwIcon className="w-3 h-3" />}>
                                    Stop &amp; reset
                                </PopoverMenuItem>
                            )}
                            <PopoverMenuSeparator />
                            <PopoverMenuItem onSelect={() => onOpen(box.id, "warmup")} icon={<RiFireLine className="w-3 h-3" />}>
                                Warmup settings
                            </PopoverMenuItem>
                            <PopoverMenuItem onSelect={() => onOpen(box.id, "deliverability")} icon={<MailCheckIcon className="w-3 h-3" />}>
                                Warmup deliverability
                            </PopoverMenuItem>
                            <PopoverMenuItem onSelect={() => onOpen(box.id, "overview")} icon={<GaugeIcon className="w-3 h-3" />}>
                                Mailbox health
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                className="w-6 h-6 flex items-center justify-center rounded hover:bg-slate-100 text-slate-400 hover:text-slate-700 transition-colors cursor-pointer"
                                aria-label="Mailbox actions"
                            >
                                <RiMoreLine className="w-3.5 h-3.5" />
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={208}>
                            {/* Health is a click on the row itself, which opens
                                the overview, so it is not repeated here. */}
                            <PopoverMenuItem onSelect={() => onOpen(box.id, "settings")} icon={<Settings2Icon className="w-3 h-3" />}>
                                Mailbox settings
                            </PopoverMenuItem>
                            {switchedOff && (
                                <PopoverMenuItem onSelect={power.switchOn} icon={<PowerIcon className="w-3 h-3" />}>
                                    Switch back on
                                </PopoverMenuItem>
                            )}
                            {box.status === "active" && (
                                <PopoverMenuItem onSelect={power.switchOff} icon={<PowerOffIcon className="w-3 h-3" />}>
                                    Switch off
                                </PopoverMenuItem>
                            )}
                            <PopoverMenuSeparator />
                            {/* The one obvious way to remove a single mailbox. It
                                used to exist only behind the row checkboxes and the
                                selection bar, which nobody finds when they want to
                                delete one thing. */}
                            <PopoverMenuItem danger onSelect={askDisconnect} icon={<Trash2Icon className="w-3 h-3" />}>
                                Disconnect mailbox
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
            </td>
        </tr>
    );
}

// A refusal the server explains with a code (a seed inbox, a blocked pool)
// reads better as its own sentence than as a generic failure.
function warmupErrorMessage(e: AppError | null | undefined): string {
    if (e?.code === "mailbox_is_seed") {
        return e.message || "This mailbox is a placement seed inbox, and seeds never warm up. Remove it from your seed inboxes first.";
    }
    if (e?.code && e.message) return e.message;
    return "Couldn't update warmup";
}

/* ── columns ─────────────────────────────────────── */

type MailboxColumnId = "mailbox" | "status" | "sent" | "warmup" | "inbox" | "health";

interface MailboxColumn {
    id: MailboxColumnId;
    label: string;
    header?: React.ReactNode;
    // Width and breakpoint, shared by the header and the row's cell.
    className: string;
    align?: "right";
    // Whether the first click sorts ascending (text) rather than descending.
    sortAsc?: boolean;
    sortValue?: (box: Inbox, status?: AccountStatus) => string | number;
}

interface MailboxSort {
    by: MailboxColumnId;
    reverse: boolean;
}

const MAILBOX_COLUMNS: MailboxColumn[] = [
    { id: "mailbox", label: "Mailbox", className: "", sortAsc: true, sortValue: (b) => b.email.toLowerCase() },
    {
        id: "status",
        label: "Status",
        header: (
            <>
                <span className="sr-only">Status</span>
                <span aria-hidden className="hidden sm:inline">Status</span>
            </>
        ),
        className: "w-16 sm:w-32",
        // Problems first, then idle, warming, sending, sending and warming.
        sortValue: (b, s) =>
            b.status !== "active" || s?.errors?.length
                ? -1
                : (s?.in_campaign ? 2 : 0) + (diagnosticWarmupActive(b) ? 1 : 0),
    },
    {
        id: "sent",
        label: "Sent today",
        header: <InfoHeader label="Sent today" title="Campaign emails sent from this mailbox today, against its daily cap." aria="How sends are counted" />,
        className: "w-28 hidden md:table-cell",
        sortValue: (_, s) => s?.daily_usage?.campaign_sent ?? -1,
    },
    {
        id: "warmup",
        label: "Warmup",
        header: <InfoHeader label="Warmup" title="Warmup emails sent today, against today's ramp target." aria="How warmup is counted" />,
        className: "w-28",
        sortValue: (b, s) => (diagnosticWarmupActive(b) ? (s?.warmup_status?.current_volume ?? 0) : -1),
    },
    {
        id: "inbox",
        label: "Inbox",
        header: (
            <InfoHeader
                label="Inbox"
                title="Share of warmup mail that reached the inbox at Google, Microsoft and Yahoo over the last 7 days. Smaller mail hosts run their own filters and are not counted."
                aria="How the inbox rate is measured"
            />
        ),
        className: "w-20 sm:w-24",
        sortValue: (_, s) => s?.warmup_placement?.inbox_rate ?? -1,
    },
    {
        id: "health",
        label: "Health",
        header: (
            <>
                <span className="sr-only">Health</span>
                <span aria-hidden className="hidden md:inline">
                    <InfoHeader label="Health" title="The mailbox's overall health score out of 100: connection, errors, bounces and warmup standing. Click it for the details." aria="How health is scored" />
                </span>
            </>
        ),
        className: "w-10 md:w-32",
        sortValue: (_, s) => s?.health?.score ?? -1,
    },
];

// A header cell; a sortable one is the sort control, like the Leads list.
function MailboxTh({ col, sort, onSort }: { col: MailboxColumn; sort: MailboxSort | null; onSort: (col: MailboxColumn) => void }) {
    const base = `px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] truncate ${col.className} ${col.align === "right" ? "text-right" : ""}`;
    const content = col.header ?? col.label;
    if (!col.sortValue) return <th className={base}>{content}</th>;
    const active = sort?.by === col.id;
    const Dir = sort?.reverse ? ArrowUpIcon : ArrowDownIcon;
    return (
        <th className={base} aria-sort={active ? (sort?.reverse ? "ascending" : "descending") : "none"}>
            <button
                type="button"
                onClick={() => onSort(col)}
                title={`Sort by ${col.label}`}
                className={`group/th inline-flex items-center gap-1 max-w-full uppercase tracking-[0.14em] hover:text-slate-700 transition-colors ${
                    active ? "text-slate-700" : ""
                } ${col.align === "right" ? "flex-row-reverse" : ""}`}
            >
                <span className="truncate">{content}</span>
                <Dir className={`w-3 h-3 shrink-0 ${active ? "" : "opacity-0 group-hover/th:opacity-60"}`} aria-hidden />
            </button>
        </th>
    );
}

// What the mailbox is doing right now. Cold sending and warmup run side by
// side, so both show when both are on; a problem that stops it wins.
function MailboxStatusPill({ box, status, warming }: { box: Inbox; status?: AccountStatus; warming: boolean }) {
    const error = status?.errors?.[0];
    const lifecycle = status?.send_lifecycle;
    const inCampaign = !!status?.in_campaign;
    const resting = inCampaign && !!lifecycle && lifecycle.state !== "active";
    const sending = inCampaign && !resting;

    let problem: { label: string; text: string; Icon: LucideIcon; title: string } | null = null;
    if (box.status === "revoked") problem = { label: "Reconnect", text: "text-rose-600", Icon: UnplugIcon, title: "Access was revoked at the provider. Reconnect the mailbox to send and warm again." };
    else if (box.status !== "active") problem = { label: "Off", text: "text-slate-500", Icon: CircleSlashIcon, title: "Switched off: it neither sends, warms nor syncs. Switch it back on from the row's menu or its Settings tab." };
    else if (error) problem = { label: "Error", text: "text-rose-600", Icon: AlertTriangleIcon, title: error.action_required ? `${error.title}. ${error.action_required}` : error.title };
    if (problem) {
        const { Icon } = problem;
        return (
            <span className={`inline-flex items-center gap-1.5 max-w-full text-[10.5px] font-medium uppercase tracking-[0.08em] ${problem.text}`} title={problem.title}>
                <Icon className="w-3 h-3 shrink-0" />
                <span className="sr-only">{problem.label}</span>
                <span aria-hidden className="hidden sm:inline truncate">{problem.label}</span>
            </span>
        );
    }

    // One word; the icons beside it say which activities are on.
    const label = sending && warming ? "Active" : sending ? "Sending" : resting ? (lifecycle?.state === "reserve" ? "Reserve" : "Resting") : warming ? "Warming" : "Idle";
    const title = [
        sending ? "Sending campaign emails" : resting ? `Held out of campaign sending${lifecycle?.reason ? `: ${lifecycle.reason}` : ""}` : "Not in a live campaign",
        warming
            ? "warming up"
            : inCampaign && diagnosticSendingAllowed(box)
              ? "low-volume diagnostic health checks are enabled"
              : box.warmup && box.warmup_paused_at
                ? "warmup paused"
                : "warmup off",
    ].join(", ");
    const text = sending ? "text-sky-700" : resting ? "text-violet-600" : warming ? "text-orange-600" : "text-slate-400";
    return (
        <span className={`inline-flex items-center gap-1.5 max-w-full text-[10.5px] font-medium uppercase tracking-[0.08em] ${text}`} title={title}>
            <span className="inline-flex items-center gap-0.5 shrink-0">
                {sending && <SendIcon className="w-3 h-3 text-sky-600" />}
                {resting && <MoonIcon className="w-3 h-3 text-violet-500" />}
                {warming && <FlameIcon className="w-3 h-3 text-orange-500" />}
                {!sending && !resting && !warming && <CircleSlashIcon className="w-3 h-3" />}
            </span>
            <span className="sr-only">{label}</span>
            <span aria-hidden className={`hidden sm:inline truncate ${sending || warming ? "text-shimmer" : ""}`}>{label}</span>
        </span>
    );
}
