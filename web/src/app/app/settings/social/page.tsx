// Settings > Social accounts: the workspace's LinkedIn, Instagram, WhatsApp, Telegram and Facebook accounts, their usage
// today and billing, connecting one (?connect=<channel> opens the dialog), and the GraphKit link.
import { useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Link } from "@tanstack/react-router";
import { CheckIcon, MoreHorizontalIcon, PauseIcon, PlayIcon, PlusIcon, RefreshCwIcon, RotateCcwIcon, SearchIcon, SettingsIcon, Trash2Icon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { NoAccess } from "@/components/layout/NoAccess";
import { Checkbox } from "@/components/ui/checkbox";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import ScrollStrip from "@/components/ui/scroll-strip";
import AccountDrawer, { type DrawerTab } from "@/components/app/social/AccountDrawer";
import ChannelMark from "@/components/app/social/ChannelMark";
import ConnectDialog from "@/components/app/social/ConnectDialog";
import GraphKitLinkCard from "@/components/app/social/GraphKitLinkCard";
import { Avatar, BillingLine, LicenceChips, Meter, StatusPill, TIER_LABEL, limitKeys, usageFor } from "@/components/app/social/parts";
import useAccountActions, { canPause, canResume, needsReconnect } from "@/components/app/social/useAccountActions";
import { usePermission } from "@/hooks/usePermission";
import useWorkspaceWritable from "@/hooks/useWorkspaceWritable";
import { useSearchParams } from "@/hooks/useSearchParams";
import { useCompleteSocialLink, useSocialAccountAction, useSocialAccounts, useSocialChannels, useSocialStatus } from "@/lib/api/hooks/app/social/useSocial";
import type { ChannelSpec, SocialAccount, SocialChannel, SocialStatus } from "@/lib/api/models/app/social/Social";
import { fmtMonthly } from "@/lib/buy/money";
import { CHANNELS, channelLabel, channelList, isChannel, orderedCatalog, rowActions } from "@/lib/social/channels";
import { socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import { Section, SectionShell } from "../_components/SectionShell";

export default function SocialAccountsPage() {
    const canManage = usePermission("MANAGE_EMAILS");
    if (!canManage) return <NoAccess feature="Social accounts" permissionLabel="Manage mailboxes" />;
    return <SocialAccounts />;
}

function priceLine(s: SocialStatus | undefined): string {
    if (!s) return "Accounts on LinkedIn, Instagram, WhatsApp, Telegram and Facebook that run beside your mailboxes.";
    if (s.mode === "linked") return `Linked to ${s.linked_workspace?.name ?? "GraphKit"}, which bills for the accounts.`;
    if (s.metered === false) return "Accounts are not charged on this instance.";
    return `${fmtMonthly(s.price.monthly_micros)} per account on any channel · ${TIER_LABEL[s.price.tier] ?? s.price.tier} pricing`;
}

function SocialAccounts() {
    const status = useSocialStatus();
    const accounts = useSocialAccounts();
    const catalog = useSocialChannels();
    const actions = useAccountActions();
    const bulk = useSocialAccountAction();
    const complete = useCompleteSocialLink();
    const { writable } = useWorkspaceWritable();
    const [params, setParams] = useSearchParams();

    const [connect, setConnect] = useState<{ open: boolean; reconnect: SocialAccount | null; channel: SocialChannel | null }>({ open: false, reconnect: null, channel: null });
    const [drawer, setDrawer] = useState<{ id: string; tab: DrawerTab } | null>(null);
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [filter, setFilter] = useState<SocialChannel | "all">("all");

    const dropParam = (key: string) =>
        setParams(
            (prev) => {
                const next = new URLSearchParams(prev);
                next.delete(key);
                return next;
            },
            { replace: true },
        );

    // GraphKit's consent page returns here with ?link_session=…; complete it once, then drop the parameter.
    const linkSession = params.get("link_session");
    const handledLink = useRef<string | null>(null);
    useEffect(() => {
        if (!linkSession || handledLink.current === linkSession) return;
        handledLink.current = linkSession;
        complete.mutate(linkSession, {
            onSuccess: (s) => toast.success(s.mode === "linked" ? `Linked to ${s.linked_workspace?.name ?? "your GraphKit workspace"}.` : "GraphKit did not finish linking."),
            onError: (e) => toast.error(socialErrorMessage(e)),
            onSettled: () => dropParam("link_session"),
        });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [linkSession, complete]);

    const s = status.data;
    const canAdd = !!s?.available && (s.can_buy || s.mode === "linked") && writable;
    const addBlocked = !s
        ? undefined
        : !s.available
          ? "Social accounts are not available on this instance."
          : !writable
            ? "This workspace is read-only right now."
            : canAdd
              ? undefined
              : s.buy_blocked_reason === "plan_required"
                ? "Adding a social account needs a Warmbly plan, or a linked GraphKit workspace."
                : "Your partner adds social accounts for this workspace.";

    const openConnect = (reconnect: SocialAccount | null = null, channel: SocialChannel | null = null) => setConnect({ open: true, reconnect, channel });

    // A setup link elsewhere lands here with ?connect=<channel> (or "any"): open the dialog once the status is known.
    const connectParam = params.get("connect");
    const handledConnect = useRef<string | null>(null);
    useEffect(() => {
        if (!connectParam || !s || handledConnect.current === connectParam) return;
        handledConnect.current = connectParam;
        if (addBlocked) toast(addBlocked);
        else openConnect(null, isChannel(connectParam) ? connectParam : null);
        dropParam("connect");
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [connectParam, s, addBlocked]);

    const all = useMemo(() => (accounts.data?.data ?? []).filter((a) => a.status !== "removed"), [accounts.data]);
    const present = useMemo(() => CHANNELS.filter((c) => all.some((a) => a.channel === c)), [all]);
    const list = filter === "all" ? all : all.filter((a) => a.channel === filter);
    const ids = list.map((a) => a.id);
    const selectedAccounts = list.filter((a) => selected.has(a.id));
    const specs = useMemo(() => new Map((catalog.data ?? []).map((c) => [c.channel, c] as const)), [catalog.data]);
    const attention = all.filter(needsReconnect).length;

    // A filter on a channel with no accounts left falls back to all.
    useEffect(() => {
        if (filter !== "all" && !present.includes(filter)) setFilter("all");
    }, [filter, present]);

    // Selection only holds accounts still listed.
    useEffect(() => {
        setSelected((prev) => {
            const live = new Set([...prev].filter((id) => ids.includes(id)));
            return live.size === prev.size ? prev : live;
        });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [ids.join(",")]);

    const toggleAll = () => setSelected(selected.size === list.length ? new Set() : new Set(ids));
    const toggle = (id: string) =>
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });

    const bulkRun = async (action: "pause" | "resume") => {
        const targets = selectedAccounts.filter(action === "pause" ? canPause : canResume);
        if (targets.length === 0) return;
        const results = await Promise.allSettled(targets.map((a) => bulk.mutateAsync({ id: a.id, action })));
        const failed = results.filter((r) => r.status === "rejected").length;
        if (failed === 0) toast.success(`${action === "pause" ? "Paused" : "Resumed"} ${targets.length} ${targets.length === 1 ? "account" : "accounts"}.`);
        else toast.error(`${failed} of ${targets.length} could not be ${action === "pause" ? "paused" : "resumed"}.`);
        setSelected(new Set());
    };

    const headerActions = (
        <>
            <button
                type="button"
                onClick={() => {
                    void status.refetch();
                    void accounts.refetch();
                }}
                aria-label="Refresh"
                className="h-7 w-7 rounded-md border border-slate-200 hover:border-slate-300 text-slate-500 hover:text-slate-900 inline-flex items-center justify-center transition-colors"
            >
                <RefreshCwIcon className={cn("w-3 h-3", accounts.isFetching && "animate-spin")} />
            </button>
            <button
                type="button"
                onClick={() => (addBlocked ? toast(addBlocked) : openConnect())}
                aria-disabled={!!addBlocked}
                title={addBlocked}
                className={cn(
                    "h-7 px-2.5 rounded-md text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors",
                    addBlocked ? "bg-slate-100 text-slate-400" : "bg-sky-600 hover:bg-sky-700 text-white",
                )}
            >
                <PlusIcon className="w-3 h-3" />
                <span className="hidden sm:inline">Connect account</span>
            </button>
        </>
    );

    return (
        <SectionShell title="Social accounts" description={priceLine(s)} actions={headerActions}>
            {status.isError && !s ? (
                <div className="px-4 md:px-8 py-16 text-center text-[12.5px] text-slate-500">Social accounts could not be loaded. Refresh to try again.</div>
            ) : s && !s.available ? (
                <Unavailable />
            ) : accounts.isPending || !s ? (
                <div className="px-4 md:px-8 py-5 space-y-2">
                    {[0, 1, 2].map((i) => (
                        <div key={i} className="h-14 rounded skeleton-shimmer" />
                    ))}
                </div>
            ) : all.length === 0 ? (
                <EmptyState status={s} catalog={catalog.data} onConnect={(c) => openConnect(null, c)} blocked={addBlocked} completing={complete.isPending} />
            ) : (
                <>
                    <Section
                        eyebrow="Accounts"
                        description={attention > 0 ? `${attention} ${attention === 1 ? "account needs" : "accounts need"} attention.` : `${all.length} ${all.length === 1 ? "account" : "accounts"}, all running.`}
                    >
                        {present.length > 1 && (
                            <ScrollStrip activeKey={filter} className="-mx-1">
                                <div className="flex items-center gap-1 px-1" role="tablist" aria-label="Filter by channel">
                                    <FilterChip active={filter === "all"} onClick={() => setFilter("all")} count={all.length}>
                                        All
                                    </FilterChip>
                                    {present.map((c) => (
                                        <FilterChip key={c} active={filter === c} onClick={() => setFilter(c)} count={all.filter((a) => a.channel === c).length}>
                                            <ChannelMark channel={c} className="size-3.5" />
                                            {channelLabel(c, catalog.data)}
                                        </FilterChip>
                                    ))}
                                </div>
                            </ScrollStrip>
                        )}
                        <div className="-mx-4 md:-mx-8 overflow-x-auto">
                            <table className="w-full min-w-[860px] text-left">
                                <thead>
                                    <tr className="h-9 border-y border-slate-200 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                        <th className="w-px whitespace-nowrap pl-4 pr-3 md:pl-8">
                                            <Checkbox
                                                className="flex"
                                                checked={selected.size > 0 && selected.size === list.length}
                                                ref={(el) => {
                                                    if (el) el.indeterminate = selected.size > 0 && selected.size < list.length;
                                                }}
                                                onChange={toggleAll}
                                                aria-label="Select every account"
                                            />
                                        </th>
                                        <th className="w-[360px] font-medium">Account</th>
                                        <th className="w-[136px] font-medium">Status</th>
                                        <th className="w-[272px] font-medium">Today</th>
                                        <th className="font-medium">Billing</th>
                                        <th className="w-12" />
                                    </tr>
                                </thead>
                                <tbody>
                                    {list.map((a) => (
                                        <AccountRow
                                            key={a.id}
                                            account={a}
                                            spec={specs.get(a.channel)}
                                            selected={selected.has(a.id)}
                                            onToggle={() => toggle(a.id)}
                                            onOpen={(tab) => setDrawer({ id: a.id, tab })}
                                            onReconnect={() => openConnect(a)}
                                            actions={actions}
                                        />
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    </Section>
                    <Section eyebrow="More">
                        <div className="grid gap-3 md:grid-cols-2">
                            <GraphKitLinkCard status={s} completing={complete.isPending} />
                            <FinderCard hasLinkedIn={all.some((a) => a.channel === "linkedin")} />
                        </div>
                    </Section>
                </>
            )}

            <AnimatePresence>
                {selected.size > 0 && (
                    <motion.div
                        initial={{ opacity: 0, y: 8 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 8 }}
                        transition={{ duration: 0.16 }}
                        className="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 flex items-center max-w-[calc(100vw-16px)] flex-wrap justify-center gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5"
                    >
                        <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                            <CheckIcon className="w-3 h-3" />
                            {selected.size} selected
                        </div>
                        <button
                            type="button"
                            disabled={!selectedAccounts.some(canPause) || bulk.isPending}
                            onClick={() => void bulkRun("pause")}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:bg-slate-100 disabled:text-slate-300 inline-flex items-center gap-1.5"
                        >
                            <PauseIcon className="w-3 h-3" /> Pause
                        </button>
                        <button
                            type="button"
                            disabled={!selectedAccounts.some(canResume) || bulk.isPending}
                            onClick={() => void bulkRun("resume")}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:bg-slate-100 disabled:text-slate-300 inline-flex items-center gap-1.5"
                        >
                            <PlayIcon className="w-3 h-3" /> Resume
                        </button>
                        <button
                            type="button"
                            onClick={() => setSelected(new Set())}
                            aria-label="Clear selection"
                            className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center"
                        >
                            <XIcon className="w-3.5 h-3.5" />
                        </button>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                {drawer && (
                    <AccountDrawer
                        key="drawer"
                        accountId={drawer.id}
                        initialTab={drawer.tab}
                        onClose={() => setDrawer(null)}
                        onReconnect={(a) => {
                            setDrawer(null);
                            openConnect(a);
                        }}
                    />
                )}
            </AnimatePresence>

            <ConnectDialog
                open={connect.open}
                onClose={() => setConnect((c) => ({ ...c, open: false }))}
                status={s}
                reconnect={connect.reconnect}
                initialChannel={connect.channel}
            />
        </SectionShell>
    );
}

function FilterChip({ active, onClick, count, children }: { active: boolean; onClick: () => void; count: number; children: React.ReactNode }) {
    return (
        <button
            type="button"
            role="tab"
            aria-selected={active}
            data-active={active ? "true" : undefined}
            onClick={onClick}
            className={cn(
                "h-7 shrink-0 px-2.5 rounded-md border text-[12px] inline-flex items-center gap-1.5 transition-colors",
                active ? "border-sky-200 bg-sky-50 text-sky-700 font-medium" : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900",
            )}
        >
            {children}
            <span className={cn("tabular-nums text-[11px]", active ? "text-sky-600" : "text-slate-400")}>{count}</span>
        </button>
    );
}

function AccountRow({
    account: a,
    spec,
    selected,
    onToggle,
    onOpen,
    onReconnect,
    actions,
}: {
    account: SocialAccount;
    spec: ChannelSpec | undefined;
    selected: boolean;
    onToggle: () => void;
    onOpen: (tab: DrawerTab) => void;
    onReconnect: () => void;
    actions: ReturnType<typeof useAccountActions>;
}) {
    const [menu, setMenu] = useState(false);
    const meters = rowActions(spec?.actions ?? limitKeys(a));
    const name = channelLabel(a.channel, spec ? [spec] : undefined);
    const sub = a.headline || a.username;
    return (
        <tr onClick={() => onOpen("overview")} className={cn("group h-16 border-b border-slate-200/60 cursor-pointer transition-colors", selected ? "bg-sky-50/40" : "hover:bg-slate-50/80")}>
            <td className="w-px pl-4 pr-3 md:pl-8" onClick={(e) => e.stopPropagation()}>
                <Checkbox className="flex" checked={selected} onChange={onToggle} aria-label={`Select ${a.display_name}`} />
            </td>
            <td className="py-3 pr-4 max-w-[360px]">
                <div className="flex items-center gap-3 min-w-0">
                    <span className="relative shrink-0">
                        <Avatar src={a.avatar_url} name={a.display_name} size={34} />
                        <span className="absolute -bottom-0.5 -right-0.5 rounded-[5px] bg-white p-px">
                            <ChannelMark channel={a.channel} className="size-3.5" />
                        </span>
                    </span>
                    <div className="min-w-0">
                        <p className="text-[12.5px] font-medium text-slate-900 truncate">{a.display_name || `${name} account`}</p>
                        {sub && <p className="text-[11.5px] text-slate-500 truncate">{sub}</p>}
                        <LicenceChips licences={a.licences} className="mt-0.5" />
                    </div>
                </div>
            </td>
            <td className="pr-4">
                <div className="flex flex-col items-start gap-1">
                    <StatusPill status={a.status} />
                    {needsReconnect(a) && a.status !== "restricted" && (
                        <button
                            type="button"
                            onClick={(e) => {
                                e.stopPropagation();
                                onReconnect();
                            }}
                            className="text-[11px] text-amber-700 hover:text-amber-800 font-medium inline-flex items-center gap-1"
                        >
                            <RotateCcwIcon className="w-3 h-3" /> Reconnect
                        </button>
                    )}
                </div>
            </td>
            <td className="pr-4">
                <div className="grid w-[248px] grid-cols-2 gap-x-4 gap-y-2">
                    {meters.map((k) => (
                        <Meter key={k} usage={usageFor(a, k)} compact />
                    ))}
                </div>
            </td>
            <td className="pr-4">
                <BillingLine account={a} />
            </td>
            <td className="pr-3 text-right" onClick={(e) => e.stopPropagation()}>
                <PopoverMenu open={menu} onOpenChange={setMenu} align="end">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            aria-label={`Actions for ${a.display_name}`}
                            className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100 transition-opacity"
                        >
                            <MoreHorizontalIcon className="w-4 h-4" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={190} role="menu">
                        <PopoverMenuItem icon={<SettingsIcon className="w-3 h-3" />} onSelect={() => onOpen("settings")}>
                            Settings
                        </PopoverMenuItem>
                        {canPause(a) && (
                            <PopoverMenuItem icon={<PauseIcon className="w-3 h-3" />} onSelect={() => actions.pause(a)}>
                                Pause
                            </PopoverMenuItem>
                        )}
                        {canResume(a) && (
                            <PopoverMenuItem icon={<PlayIcon className="w-3 h-3" />} onSelect={() => actions.resume(a)}>
                                Resume
                            </PopoverMenuItem>
                        )}
                        <PopoverMenuItem icon={<RotateCcwIcon className="w-3 h-3" />} onSelect={onReconnect}>
                            Reconnect
                        </PopoverMenuItem>
                        <PopoverMenuItem icon={<RefreshCwIcon className="w-3 h-3" />} onSelect={() => actions.resync(a)}>
                            Resync
                        </PopoverMenuItem>
                        <PopoverMenuSeparator />
                        <PopoverMenuItem danger icon={<Trash2Icon className="w-3 h-3" />} onSelect={() => actions.remove(a)}>
                            Remove
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>
            </td>
        </tr>
    );
}

function EmptyState({
    status,
    catalog,
    onConnect,
    blocked,
    completing,
}: {
    status: SocialStatus;
    catalog: ChannelSpec[] | undefined;
    onConnect: (channel: SocialChannel | null) => void;
    blocked: string | undefined;
    completing: boolean;
}) {
    const channels = orderedCatalog(catalog);
    const consent = channels.filter((c) => c.consent_required).map((c) => c.channel);
    return (
        <>
            <Section eyebrow="Connect an account" description="Each account runs its own paced actions in your sequences, and its replies land in the Unibox beside your email.">
                <div className="grid gap-2 grid-cols-2 sm:grid-cols-3 lg:grid-cols-5">
                    {(channels.length ? channels.map((c) => c.channel) : CHANNELS).map((c) => (
                        <button
                            key={c}
                            type="button"
                            disabled={!!blocked}
                            onClick={() => onConnect(c)}
                            className="flex flex-col items-start gap-1.5 rounded-lg border border-slate-200 px-3 py-2.5 text-left transition-colors hover:border-slate-300 disabled:opacity-60 disabled:hover:border-slate-200"
                        >
                            <ChannelMark channel={c} className="size-5" />
                            <span className="text-[12.5px] font-medium text-slate-900">{channelLabel(c, catalog)}</span>
                            <span className="inline-flex items-center gap-1 text-[11px] text-sky-700">
                                <PlusIcon className="size-3" /> Connect
                            </span>
                        </button>
                    ))}
                </div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    {status.mode === "linked"
                        ? "Your GraphKit workspace pays for the accounts."
                        : status.metered === false
                          ? "Accounts are not charged on this instance."
                          : `${fmtMonthly(status.price.monthly_micros)} per account on any channel, paid from your balance first.`}
                    {consent.length > 0 && ` ${channelList(consent)} only reach contacts who agreed to hear from you there.`}
                </p>
                {blocked && (
                    <p className="text-[11.5px] text-slate-500">
                        {blocked}{" "}
                        {status.buy_blocked_reason === "plan_required" && (
                            <Link to="/app/settings/billing/{-$tab}" params={{ tab: "plans" }} className="text-sky-700 hover:text-sky-800 font-medium">
                                Choose a plan
                            </Link>
                        )}
                    </p>
                )}
            </Section>
            <Section eyebrow="Use your own GraphKit workspace">
                <GraphKitLinkCard status={status} completing={completing} />
            </Section>
        </>
    );
}

function FinderCard({ hasLinkedIn }: { hasLinkedIn: boolean }) {
    return (
        <Link
            to="/app/leads"
            search={{ source: "linkedin" }}
            className="rounded-lg border border-slate-200 bg-white px-4 py-3.5 flex items-start gap-3 hover:border-slate-300 transition-colors"
        >
            <span className="size-8 rounded-md bg-sky-50 text-sky-600 inline-flex items-center justify-center shrink-0">
                <SearchIcon className="w-4 h-4" />
            </span>
            <div className="min-w-0">
                <p className="text-[12.5px] font-semibold text-slate-900">Lead finder</p>
                <p className="mt-0.5 text-[11.5px] text-slate-500 leading-relaxed">
                    {hasLinkedIn
                        ? "Search LinkedIn, Sales Navigator or Recruiter with your LinkedIn accounts and import who you find into contacts."
                        : "Lead search runs on LinkedIn. Connect a LinkedIn account to search Sales Navigator or Recruiter from the Lead finder."}
                </p>
            </div>
        </Link>
    );
}

function Unavailable() {
    return (
        <div className="px-4 md:px-8 py-16 flex flex-col items-center text-center">
            <div className="flex items-center gap-1.5 opacity-60">
                {CHANNELS.map((c) => (
                    <ChannelMark key={c} channel={c} className="size-6" />
                ))}
            </div>
            <p className="mt-3 text-[13px] font-medium text-slate-800">Social accounts are not set up on this instance</p>
            <p className="mt-1 text-[12px] text-slate-500 max-w-[46ch] leading-relaxed">
                Social accounts run through GraphKit. Once this instance&apos;s operator connects it, accounts can be added here.
            </p>
        </div>
    );
}
