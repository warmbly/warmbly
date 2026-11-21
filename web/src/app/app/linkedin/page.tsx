// LinkedIn: the workspace's connected LinkedIn accounts, their usage today and billing, connecting one, and the GraphKit link.
import { useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Link } from "@tanstack/react-router";
import {
    CheckIcon,
    InboxIcon,
    MessageSquareIcon,
    MoreHorizontalIcon,
    PauseIcon,
    PlayIcon,
    PlusIcon,
    RefreshCwIcon,
    RotateCcwIcon,
    SearchIcon,
    SettingsIcon,
    ShieldCheckIcon,
    Trash2Icon,
    UserPlusIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast/headless";
import { NoAccess } from "@/components/layout/NoAccess";
import { Page, PageBody, PageTopbar, SectionBar, Stat, StatStrip, TopbarAction } from "@/components/layout/Page";
import { Checkbox } from "@/components/ui/checkbox";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import AccountDrawer, { type DrawerTab } from "@/components/app/linkedin/AccountDrawer";
import ConnectDialog from "@/components/app/linkedin/ConnectDialog";
import GraphKitLinkCard from "@/components/app/linkedin/GraphKitLinkCard";
import LinkedInMark from "@/components/app/linkedin/LinkedInMark";
import { Avatar, BillingLine, LicenceChips, Meter, ROW_ACTIONS, StatusPill, TIER_LABEL, usageFor } from "@/components/app/linkedin/parts";
import useAccountActions, { canPause, canResume, needsReconnect } from "@/components/app/linkedin/useAccountActions";
import { usePermission } from "@/hooks/usePermission";
import { useSearchParams } from "@/hooks/useSearchParams";
import { useCompleteLinkedInLink, useLinkedInAccountAction, useLinkedInAccounts, useLinkedInStatus } from "@/lib/api/hooks/app/linkedin/useLinkedIn";
import type { LinkedInAccount, LinkedInStatus } from "@/lib/api/models/app/linkedin/LinkedIn";
import { fmtMonthly } from "@/lib/buy/money";
import { linkedInErrorMessage } from "@/lib/linkedin/errors";
import { cn } from "@/lib/utils";

export default function LinkedInPage() {
    const canManage = usePermission("MANAGE_EMAILS");
    if (!canManage) return <NoAccess feature="LinkedIn" permissionLabel="Manage mailboxes" />;
    return <LinkedInAccounts />;
}

function modeLabel(s: LinkedInStatus | undefined): string {
    if (!s) return "";
    if (s.mode === "linked") return `Linked to ${s.linked_workspace?.name ?? "GraphKit"}`;
    return `${fmtMonthly(s.price.monthly_micros)} per account · ${TIER_LABEL[s.price.tier] ?? s.price.tier} pricing`;
}

function LinkedInAccounts() {
    const status = useLinkedInStatus();
    const accounts = useLinkedInAccounts();
    const actions = useAccountActions();
    const bulk = useLinkedInAccountAction();
    const complete = useCompleteLinkedInLink();
    const [params, setParams] = useSearchParams();

    const [connectOpen, setConnectOpen] = useState(false);
    const [reconnect, setReconnect] = useState<LinkedInAccount | null>(null);
    const [drawer, setDrawer] = useState<{ id: string; tab: DrawerTab } | null>(null);
    const [selected, setSelected] = useState<Set<string>>(new Set());

    // GraphKit's consent page returns here with ?link_session=…; complete it once, then drop the parameter.
    const linkSession = params.get("link_session");
    const handled = useRef<string | null>(null);
    useEffect(() => {
        if (!linkSession || handled.current === linkSession) return;
        handled.current = linkSession;
        complete.mutate(linkSession, {
            onSuccess: (s) => toast.success(s.mode === "linked" ? `Linked to ${s.linked_workspace?.name ?? "your GraphKit workspace"}.` : "GraphKit did not finish linking."),
            onError: (e) => toast.error(linkedInErrorMessage(e)),
            onSettled: () =>
                setParams(
                    (prev) => {
                        const next = new URLSearchParams(prev);
                        next.delete("link_session");
                        return next;
                    },
                    { replace: true },
                ),
        });
    }, [linkSession, complete, setParams]);

    const list = useMemo(() => (accounts.data?.data ?? []).filter((a) => a.status !== "removed"), [accounts.data]);
    const s = status.data;
    const ids = list.map((a) => a.id);
    const selectedAccounts = list.filter((a) => selected.has(a.id));

    // Selection only holds accounts still listed.
    useEffect(() => {
        setSelected((prev) => {
            const live = new Set([...prev].filter((id) => ids.includes(id)));
            return live.size === prev.size ? prev : live;
        });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [ids.join(",")]);

    const canAdd = !!s?.available && (s.can_buy || s.mode === "linked");
    const addBlocked = !s
        ? undefined
        : !s.available
          ? "LinkedIn is not available on this instance."
          : canAdd
            ? undefined
            : s.buy_blocked_reason === "plan_required"
              ? "Adding a LinkedIn account needs a Warmbly plan, or a linked GraphKit workspace."
              : "Your partner adds LinkedIn accounts for this workspace.";

    const openConnect = (a: LinkedInAccount | null = null) => {
        setReconnect(a);
        setConnectOpen(true);
    };

    const usageTotal = (action: string) => list.reduce((n, a) => n + usageFor(a, action).today, 0);
    const attention = list.filter(needsReconnect).length;

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
        setSelected(new Set());
    };

    return (
        <Page>
            <PageTopbar eyebrow="LinkedIn" subtitle={modeLabel(s)}>
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
                <TopbarAction
                    writes
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => (addBlocked ? toast(addBlocked) : openConnect())}
                    heldReason={addBlocked}
                >
                    <span className="hidden sm:inline">Connect account</span>
                </TopbarAction>
            </PageTopbar>

            {list.length > 0 && (
                <StatStrip cols={4}>
                    <Stat label="Accounts" value={list.length} sub={attention > 0 ? `${attention} need attention` : "All running"} accent={attention === 0} tone={attention > 0 ? "warn" : undefined} />
                    <Stat label="Invites today" value={usageTotal("invite")} sub="Across every account" />
                    <Stat label="Messages today" value={usageTotal("message")} sub={`${usageTotal("inmail")} InMail`} />
                    <Stat label="Profile visits today" value={usageTotal("profile_visit")} last />
                </StatStrip>
            )}

            <PageBody>
                {status.isError && !s ? (
                    <div className="px-5 py-16 text-center text-[12.5px] text-slate-500">LinkedIn could not be loaded. Refresh to try again.</div>
                ) : s && !s.available ? (
                    <Unavailable />
                ) : accounts.isPending || !s ? (
                    <div className="px-5 py-4 space-y-2">
                        {[0, 1, 2].map((i) => (
                            <div key={i} className="h-14 rounded skeleton-shimmer" />
                        ))}
                    </div>
                ) : list.length === 0 ? (
                    <EmptyState status={s} onConnect={() => openConnect()} blocked={addBlocked} completing={complete.isPending} />
                ) : (
                    <>
                        <SectionBar label="Accounts" count={list.length} />
                        <div className="overflow-x-auto">
                            <table className="w-full min-w-[860px] text-left">
                                <thead>
                                    <tr className="h-9 border-b border-slate-200 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                        <th className="w-10 pl-5">
                                            <Checkbox
                                                checked={selected.size > 0 && selected.size === list.length}
                                                ref={(el) => {
                                                    if (el) el.indeterminate = selected.size > 0 && selected.size < list.length;
                                                }}
                                                onChange={toggleAll}
                                                aria-label="Select every account"
                                            />
                                        </th>
                                        <th className="font-medium">Account</th>
                                        <th className="font-medium">Status</th>
                                        <th className="font-medium">Today</th>
                                        <th className="font-medium">Billing</th>
                                        <th className="w-12" />
                                    </tr>
                                </thead>
                                <tbody>
                                    {list.map((a) => (
                                        <AccountRow
                                            key={a.id}
                                            account={a}
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
                        <div className="px-5 py-5 grid gap-3 md:grid-cols-2">
                            <GraphKitLinkCard status={s} completing={complete.isPending} />
                            <FinderCard />
                        </div>
                    </>
                )}
            </PageBody>

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

            <ConnectDialog open={connectOpen} onClose={() => setConnectOpen(false)} status={s} reconnect={reconnect} />
        </Page>
    );
}

function AccountRow({
    account: a,
    selected,
    onToggle,
    onOpen,
    onReconnect,
    actions,
}: {
    account: LinkedInAccount;
    selected: boolean;
    onToggle: () => void;
    onOpen: (tab: DrawerTab) => void;
    onReconnect: () => void;
    actions: ReturnType<typeof useAccountActions>;
}) {
    const [menu, setMenu] = useState(false);
    return (
        <tr onClick={() => onOpen("overview")} className={cn("group h-16 border-b border-slate-200/60 cursor-pointer transition-colors", selected ? "bg-sky-50/40" : "hover:bg-slate-50/80")}>
            <td className="pl-5" onClick={(e) => e.stopPropagation()}>
                <Checkbox checked={selected} onChange={onToggle} aria-label={`Select ${a.display_name}`} />
            </td>
            <td className="pr-4 max-w-[320px]">
                <div className="flex items-center gap-3 min-w-0">
                    <Avatar src={a.avatar_url} name={a.display_name} size={34} />
                    <div className="min-w-0">
                        <p className="text-[12.5px] font-medium text-slate-900 truncate">{a.display_name || "LinkedIn account"}</p>
                        {a.headline && <p className="text-[11.5px] text-slate-500 truncate">{a.headline}</p>}
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
                <div className="flex items-center gap-3">
                    {ROW_ACTIONS.map((k) => (
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

const PITCH = [
    { icon: UserPlusIcon, title: "Invites and messages in your sequences", body: "Connection requests, messages, InMail and profile visits run next to your email steps." },
    { icon: InboxIcon, title: "Replies land in the Unibox", body: "LinkedIn conversations sit with your email threads, matched to the same contact." },
    { icon: SearchIcon, title: "Find leads on LinkedIn", body: "Search Sales Navigator, Recruiter or LinkedIn from the Lead finder and import straight into contacts." },
    { icon: ShieldCheckIcon, title: "Paced like a person", body: "Daily limits capped per licence, working hours and a gradual warm-up for each account." },
];

function EmptyState({ status, onConnect, blocked, completing }: { status: LinkedInStatus; onConnect: () => void; blocked: string | undefined; completing: boolean }) {
    return (
        <div className="px-5 py-10 max-w-[880px] mx-auto">
            <div className="flex flex-col items-center text-center">
                <motion.div initial={{ scale: 0.9, opacity: 0 }} animate={{ scale: 1, opacity: 1 }} transition={{ type: "spring", stiffness: 300, damping: 22 }}>
                    <LinkedInMark className="w-11 h-11" />
                </motion.div>
                <h1 className="mt-3 text-[17px] font-semibold tracking-[-0.01em] text-slate-900">Add LinkedIn to your outreach</h1>
                <p className="mt-1.5 text-[12.5px] text-slate-500 max-w-[52ch] leading-relaxed">
                    Connect a LinkedIn account and run it beside your mailboxes.{" "}
                    {status.mode === "linked" ? "Your GraphKit workspace pays for its seats." : `${fmtMonthly(status.price.monthly_micros)} per account, paid from your balance first.`}
                </p>
                <button
                    type="button"
                    onClick={onConnect}
                    disabled={!!blocked}
                    className="mt-4 h-8 px-3.5 rounded-md bg-sky-600 hover:bg-sky-700 disabled:bg-slate-200 disabled:text-slate-400 text-white text-[12.5px] font-semibold inline-flex items-center gap-1.5"
                >
                    <PlusIcon className="w-3.5 h-3.5" /> Connect a LinkedIn account
                </button>
                {blocked && (
                    <p className="mt-2 text-[11.5px] text-slate-500">
                        {blocked}{" "}
                        {status.buy_blocked_reason === "plan_required" && (
                            <Link to="/app/settings/billing/{-$tab}" params={{ tab: "plans" }} className="text-sky-700 hover:text-sky-800 font-medium">
                                Choose a plan
                            </Link>
                        )}
                    </p>
                )}
            </div>
            <div className="mt-8 grid gap-3 sm:grid-cols-2">
                {PITCH.map((p) => (
                    <div key={p.title} className="rounded-lg border border-slate-200 px-3.5 py-3 flex items-start gap-3">
                        <span className="size-7 rounded-md bg-sky-50 text-sky-600 inline-flex items-center justify-center shrink-0">
                            <p.icon className="w-3.5 h-3.5" />
                        </span>
                        <div className="min-w-0">
                            <p className="text-[12.5px] font-medium text-slate-900">{p.title}</p>
                            <p className="mt-0.5 text-[11.5px] text-slate-500 leading-relaxed">{p.body}</p>
                        </div>
                    </div>
                ))}
            </div>
            <GraphKitLinkCard status={status} completing={completing} className="mt-6" />
        </div>
    );
}

function FinderCard() {
    return (
        <Link
            to="/app/leads"
            search={{ source: "linkedin" }}
            className="rounded-lg border border-slate-200 bg-white px-4 py-3.5 flex items-start gap-3 hover:border-slate-300 transition-colors"
        >
            <span className="size-8 rounded-md bg-sky-50 text-sky-600 inline-flex items-center justify-center shrink-0">
                <MessageSquareIcon className="w-4 h-4" />
            </span>
            <div className="min-w-0">
                <p className="text-[12.5px] font-semibold text-slate-900">Find people on LinkedIn</p>
                <p className="mt-0.5 text-[11.5px] text-slate-500 leading-relaxed">Search with these accounts in the Lead finder and import who you find into contacts.</p>
            </div>
        </Link>
    );
}

function Unavailable() {
    return (
        <div className="px-5 py-16 flex flex-col items-center text-center">
            <LinkedInMark className="w-10 h-10 opacity-60" />
            <p className="mt-3 text-[13px] font-medium text-slate-800">LinkedIn is not set up on this instance</p>
            <p className="mt-1 text-[12px] text-slate-500 max-w-[46ch] leading-relaxed">
                LinkedIn accounts run through GraphKit. Once this instance&apos;s operator connects it, accounts can be added here.
            </p>
        </div>
    );
}
