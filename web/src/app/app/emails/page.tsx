import { RiFireLine, RiMoreLine } from "@remixicon/react";
import React, { useEffect, useMemo, useRef } from "react";
import { useSearchParams } from "react-router-dom";
import toast from "react-hot-toast";
import { useQueryClient } from "@tanstack/react-query";
import useEmails from "@/lib/api/hooks/app/emails/useEmails";
import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import useWarmupLifecycle from "@/lib/api/hooks/app/emails/useWarmupLifecycle";
import useAccountStatuses from "@/lib/api/hooks/app/analytics/useAccountStatuses";
import useFeatureStatus from "@/lib/api/hooks/app/subscription/useFeatureStatus";
import warmupLifecycle from "@/lib/api/client/app/emails/warmupLifecycle";
import removeEmail from "@/lib/api/client/app/emails/removeEmail";
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
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";
import BulkWarmupDialog from "@/components/app/emails/BulkWarmupDialog";
import BulkTagPopover from "@/components/app/emails/BulkTagPopover";
import MailboxImportsMenu from "@/components/app/emails/import/MailboxImportsMenu";
import MailboxSourceChip from "@/components/app/emails/MailboxSourceChip";
import SigninMigrationBanner, { SigninRetiringChip } from "@/components/app/emails/migration/SigninMigrationBanner";
import SigninMigrationDialog from "@/components/app/emails/migration/SigninMigrationDialog";
import MailboxGrantDialog from "@/components/app/emails/import/grants/MailboxGrantDialog";
import { useSigninMigration } from "@/lib/api/hooks/app/emails/useMailboxGrants";
import { mailboxSource } from "@/lib/mailboxSource";
import type Tag from "@/lib/api/models/app/Tag";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import mailboxDisplayStatus from "@/lib/mailboxStatus";
import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import {
    ActivityIcon,
    CheckIcon,
    FilterIcon,
    GaugeIcon,
    GlobeIcon,
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

import AdvisorRowFlag from "@/components/app/advisor/AdvisorRowFlag";
import AdvisorSummaryBar from "@/components/app/advisor/AdvisorSummaryBar";
import { useAdvisorEntityIndex } from "@/lib/api/hooks/app/advisor/useAdvisor";
import type { AdvisorFinding } from "@/lib/api/models/app/advisor/Advisor";
import { Checkbox } from "@/components/ui/checkbox";

export default function AddressesPage() {
    const p = useUserProfile();
    const confirm = useConfirm();
    const canView = usePermission("MANAGE_EMAILS");

    const [query, setQuery] = React.useState<string>("");
    const [tag, setTag] = React.useState<string>("");
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
        if (failed > 0) toast.error(`${failed} mailbox${failed > 1 ? "es" : ""} couldn't be updated`);
        else toast.success(`Warmup ${verb} for ${n} mailbox${n > 1 ? "es" : ""}`);
    };

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
        () => (emailsData.emails ?? []).filter((e) => !!e.warmup && !e.warmup_paused_at).length,
        [emailsData.emails],
    );

    function isSelectedAll(): boolean {
        return emailsData.emails
            ? emailsData.emails.length > 0 && selected.length === emailsData.emails.length
            : false;
    }

    if (!canView) {
        return <NoAccess feature="email accounts" permissionLabel="Manage mailboxes" />;
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
                    {!emailsData.isLoading && <CloudPathsPanel mailboxCount={stats.total} warmingCount={warmupActive} onAdd={() => p?.setAddEmail(true)} />}
                    {/* Hosted, the pool is thousands of mailboxes: the pool-size advice is self-host only. */}
                    {cloud.selfHosted && (
                        <WarmupCoverageNotice
                            warmupCount={warmupActive}
                            totalCount={stats.total}
                            canWarmup={canWarmup}
                            onAdd={() => p?.setAddEmail(true)}
                            onConnectCloud={!cloud.connected ? () => setCloudDialog(true) : undefined}
                            cloudConnected={cloud.connected}
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
                    <table className="w-full text-left">
                        <thead className="sticky top-0 bg-white z-[1]">
                            <tr className="border-b border-slate-200">
                                <th className="pl-5 pr-2 py-2 w-9">
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
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em]">Account</th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-24 text-right">Warmup</th>
                                <th
                                    className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-16 md:w-20 text-right"
                                    title="Share of warmup mail that reached the inbox over the last 7 days, measured in partners' mailboxes"
                                >
                                    Inbox
                                </th>
                                <th className="px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] w-10 md:w-32"><span className="hidden md:inline">Health</span></th>
                                <th className="px-3 py-2 w-16"></th>
                            </tr>
                        </thead>
                        <tbody>
                            {emailsData.emails.map((box) => (
                                <MailboxRow
                                    key={box.id}
                                    box={box}
                                    tags={p?.user.tags ?? []}
                                    status={statusById.get(box.id)}
                                    findings={advisor.get(box.id)}
                                    canWarmup={canWarmup}
                                    cloud={cloud.connected ? cloud.rowFor(box.id) : undefined}
                                    cloudConnected={cloud.connected}
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
                            Pause
                        </button>
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

    const off = !box.warmup;
    const paused = !!box.warmup && !!box.warmup_paused_at;
    const active = !!box.warmup && !box.warmup_paused_at;

    const tone = healthTone(status);
    const ws = status?.warmup_status;
    const inCampaign = status?.in_campaign;

    const cloudEnroll = useEnrollCloudLinkMailbox();
    const cloudUnenroll = useUnenrollCloudLinkMailbox();
    const cloudLifecycle = useCloudLinkMailboxLifecycle();
    const inCloud = !!cloud?.enrolled;
    const cloudPaused = !!cloud?.cloud?.warmup?.paused;
    const cloudSupported = providerSupported(box.provider);
    const cloudRun = async (fn: () => Promise<unknown>, ok: string) => {
        try {
            await fn();
            toast.success(ok);
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    };

    // Warmup column: what's flowing today and why.
    const warmupLabel = inCloud
        ? cloud?.cloud
            ? `${cloud.cloud.sent_today}/${cloud.cloud.warmup?.target_volume ?? cloud.cloud.settings.base}`
            : "Cloud"
        : active
        ? `${ws?.current_volume ?? 0}/${ws?.target_volume ?? box.warmup_base}`
        : paused
            ? "Paused"
            : inCampaign
                ? "Health-check"
                : "Off";
    const warmupTone = inCloud
        ? cloudPaused
            ? "text-amber-600"
            : "text-sky-600"
        : active
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
                <Checkbox
                    checked={checked}
                    onChange={onToggleSelect}
                    onClick={(e) => e.stopPropagation()}
                />
            </td>
            <td className="px-3 max-w-0 md:max-w-none">
                {/* The flag is a sibling of the open-row button, not a child:
                    it has its own trigger and nesting buttons is invalid. */}
                <div className="flex w-full min-w-0 items-center gap-2">
                <button type="button" onClick={(e) => { e.stopPropagation(); onOpen(box.id); }} className="flex min-w-0 flex-1 items-center gap-2.5 text-left">
                    <div className="w-6 h-6 rounded-full bg-sky-100 flex items-center justify-center shrink-0">
                        <span className="text-[9.5px] font-semibold text-sky-700">
                            {box.email.slice(0, 2).toUpperCase()}
                        </span>
                    </div>
                    <span className="text-[12.5px] font-medium text-slate-900 truncate">{box.email}</span>
                    <MailboxSourceChip box={box} labelClassName={mailboxSource(box).kind === "host" ? "hidden lg:inline" : "hidden md:inline"} />
                    {inCloud && (
                        <span
                            title={cloud?.managed ? "Signed in through Warmbly Cloud, which warms it" : cloudPaused ? "Paused in Warmbly Cloud" : "Warmed by Warmbly Cloud"}
                            className={`inline-flex items-center gap-1 h-4 px-1.5 rounded-full text-[9.5px] font-medium uppercase tracking-[0.08em] shrink-0 ${cloudPaused ? "bg-amber-50 text-amber-600" : "bg-sky-600 text-white"}`}
                        >
                            <CloudIcon className="w-2.5 h-2.5" /> Cloud
                        </span>
                    )}
                    {inCampaign && (
                        <span className="hidden sm:inline-flex items-center gap-1 h-4 px-1.5 rounded-full bg-sky-50 text-sky-600 text-[9.5px] font-medium uppercase tracking-[0.08em]">
                            <ActivityIcon className="w-2.5 h-2.5" /> In campaign
                        </span>
                    )}
                    {shownTags.map((t) => (
                        <span
                            key={t.id}
                            className="hidden md:inline-flex items-center gap-1 h-4 px-1.5 rounded-full text-[9.5px] font-medium shrink-0"
                            style={{ backgroundColor: `${t.color}1a`, color: t.color }}
                        >
                            <span className="size-1.5 rounded-full" style={{ backgroundColor: t.color }} />
                            {t.title}
                        </span>
                    ))}
                    {rowTags.length > shownTags.length && (
                        <span className="hidden md:inline-flex items-center h-4 px-1 rounded-full bg-slate-100 text-slate-500 text-[9.5px] font-medium shrink-0">
                            +{rowTags.length - shownTags.length}
                        </span>
                    )}
                </button>
                {retiring && <SigninRetiringChip onClick={onRetiring} />}
                <AdvisorRowFlag findings={findings} subject={box.email} />
                </div>
            </td>
            <td className={`px-3 text-[12px] tabular-nums text-right font-mono ${warmupTone}`}>
                {inCloud ? (
                    <span className="inline-flex items-center justify-end gap-1.5">
                        <CloudIcon className="w-3 h-3 shrink-0" />
                        <span>{warmupLabel}</span>
                    </span>
                ) : active ? (
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
            <td className="px-3 text-right">
                <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); onOpen(box.id, "deliverability"); }}
                    aria-label="View warmup deliverability"
                >
                    <PlacementRateBadge rate={status?.warmup_placement} />
                </button>
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
                    <span className="uppercase tracking-[0.08em] hidden md:inline">{tone.label}</span>
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
                                {inCloud ? <CloudIcon className={`w-3.5 h-3.5 ${cloudPaused ? "text-amber-500" : "text-sky-600"}`} /> : <RiFireLine className={`w-3.5 h-3.5 ${active ? "text-orange-500" : paused ? "text-amber-500" : ""}`} />}
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={208}>
                            <PopoverMenuLabel>Warmup · {inCloud ? (cloudPaused ? "Paused in cloud" : "Warmbly Cloud") : active ? "Active" : paused ? "Paused" : "Off"}</PopoverMenuLabel>
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
