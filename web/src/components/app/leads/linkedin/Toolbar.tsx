// The finder's search bar, as on LinkedIn: the account, the source (locked ones explain themselves), the keywords and Open in LinkedIn.
import { ChevronDownIcon, ExternalLinkIcon, InfoIcon, LinkIcon, LockIcon, SearchIcon, SlidersHorizontalIcon, UsersIcon } from "lucide-react";
import type { Dispatch, RefObject } from "react";
import { Avatar } from "@/components/app/social/parts";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuLabel, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import ScrollStrip from "@/components/ui/scroll-strip";
import type { FinderCatalog, FinderSource } from "@/lib/api/models/app/social/Social";
import { profileUrl } from "@/lib/social/channels";
import { licenceLabel } from "@/components/app/social/parts";
import { cn } from "@/lib/utils";
import { Tip } from "./bits";
import { accountCan, AUTO_ACCOUNT, poolName, poolSummary, type FinderAccount, type FinderAction, type FinderState, type KeywordCheck } from "./state";

const SOURCE_ORDER: FinderSource[] = ["sales_navigator_leads", "sales_navigator_accounts", "classic_people", "recruiter_people"];
const SHORT: Record<FinderSource, string> = { sales_navigator_leads: "Leads", sales_navigator_accounts: "Accounts", classic_people: "People", recruiter_people: "Recruiter" };
const LONG: Record<FinderSource, string> = {
    sales_navigator_leads: "Sales Navigator leads",
    sales_navigator_accounts: "Sales Navigator accounts",
    classic_people: "LinkedIn people",
    recruiter_people: "Recruiter",
};

export const sourceNeed = (s: FinderSource) => (s.startsWith("sales_navigator") ? "a Sales Navigator seat" : s === "recruiter_people" ? "a Recruiter seat" : "an active LinkedIn account");

const fmt = (n: number) => n.toLocaleString("en-US");

export function AccountSelect({ accounts, value, source, onChange }: { accounts: FinderAccount[] | undefined; value: string; source: FinderSource; onChange: (id: string) => void }) {
    if (!accounts) return <span className="h-7 w-48 rounded-md bg-slate-100 animate-pulse" />;
    const pool = poolSummary(accounts, source);
    const current = accounts.find((a) => a.id === value);
    return (
        <PopoverMenu>
            <PopoverMenuTrigger asChild>
                <button type="button" aria-label="LinkedIn account" className="h-7 max-w-[240px] px-2 rounded-md border border-slate-200 bg-white text-[12px] text-slate-800 hover:border-slate-300 inline-flex items-center gap-1.5">
                    {value === AUTO_ACCOUNT ? <UsersIcon className="size-3.5 text-slate-400" /> : current ? <Avatar src={current.avatar_url} name={current.display_name} size={16} /> : null}
                    <span className="truncate">{value === AUTO_ACCOUNT ? `Any ${poolName(source)} account` : (current?.display_name ?? "Choose an account")}</span>
                    <ChevronDownIcon className="size-3.5 shrink-0 text-slate-400" />
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={300}>
                <PopoverMenuLabel>Search with</PopoverMenuLabel>
                {pool.count >= 2 && (
                    <PopoverMenuItem
                        icon={<UsersIcon className="size-3.5" />}
                        selected={value === AUTO_ACCOUNT}
                        onSelect={() => onChange(AUTO_ACCOUNT)}
                        description={`${pool.count} accounts · ${fmt(pool.left)} of ${fmt(pool.limit)} results left today`}
                    >
                        Any {poolName(source)} account
                    </PopoverMenuItem>
                )}
                {accounts.map((a) => {
                    const ok = accountCan(a, source);
                    return (
                        <PopoverMenuItem
                            key={a.id}
                            disabled={a.status !== "active"}
                            selected={value === a.id}
                            onSelect={() => onChange(a.id)}
                            icon={<Avatar src={a.avatar_url} name={a.display_name} size={18} />}
                            description={
                                a.status !== "active"
                                    ? "Not active"
                                    : `${a.licences.map(licenceLabel).join(" · ") || "LinkedIn"}${ok ? "" : " · cannot search this source"} · ${fmt(a.usage.used)}/${fmt(a.usage.limit)} today`
                            }
                        >
                            {a.display_name}
                        </PopoverMenuItem>
                    );
                })}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}

export function FinderToolbar({
    accounts,
    accountId,
    onAccount,
    state,
    dispatch,
    onSource,
    catalog,
    kw,
    keywordsRef,
    linkedInUrl,
    filterCount,
    onOpenFilters,
    sourceAvailable,
}: {
    accounts: FinderAccount[] | undefined;
    accountId: string;
    onAccount: (id: string) => void;
    state: FinderState;
    dispatch: Dispatch<FinderAction>;
    onSource: (s: FinderSource) => void;
    catalog: FinderCatalog | undefined;
    kw: KeywordCheck;
    keywordsRef: RefObject<HTMLInputElement | null>;
    linkedInUrl: string | null;
    filterCount: number;
    onOpenFilters: () => void;
    sourceAvailable: (s: FinderSource) => boolean;
}) {
    const href = profileUrl("linkedin", linkedInUrl);
    return (
        <div className="shrink-0 grid gap-2 border-b border-slate-200 bg-white px-3 py-2 md:px-4">
            <div className="flex flex-wrap items-center gap-2">
                <AccountSelect accounts={accounts} value={accountId} source={state.source} onChange={onAccount} />
                <ScrollStrip activeKey={state.source} className="min-w-0 max-w-full" innerClassName="inline-flex rounded-md border border-slate-200 p-0.5">
                    {SOURCE_ORDER.map((s) => {
                        // Locked sources stay clickable: choosing one explains how to unlock it.
                        const ok = !accounts || sourceAvailable(s);
                        const on = state.source === s;
                        return (
                            <button
                                key={s}
                                type="button"
                                data-active={on ? "true" : undefined}
                                aria-pressed={on}
                                title={ok ? LONG[s] : `Needs ${sourceNeed(s)} on one of your accounts`}
                                onClick={() => onSource(s)}
                                className={cn("h-6 px-2 rounded inline-flex shrink-0 items-center gap-1 text-[11.5px] whitespace-nowrap", on ? "bg-slate-100 text-slate-900 font-medium" : "text-slate-500 hover:text-slate-900")}
                            >
                                {!ok && <LockIcon aria-label="Locked" className="size-3 opacity-70" />}
                                <span className="sm:hidden">{SHORT[s]}</span>
                                <span className="max-sm:hidden">{LONG[s]}</span>
                            </button>
                        );
                    })}
                </ScrollStrip>
            </div>
            <div className="flex items-start gap-2">
                <button
                    type="button"
                    onClick={onOpenFilters}
                    className="lg:hidden h-7 px-2 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 inline-flex items-center gap-1.5"
                    aria-label="Filters"
                >
                    <SlidersHorizontalIcon className="size-3.5" /> Filters
                    {filterCount > 0 && <span className="rounded bg-slate-900 px-1 text-[10.5px] text-white tabular-nums">{filterCount}</span>}
                </button>
                <div className="grid min-w-0 flex-1 gap-1">
                    <div className={cn("flex h-7 items-center gap-2 rounded-md border bg-white px-2 focus-within:ring-2", kw.ok ? "border-slate-200 focus-within:border-sky-400 focus-within:ring-sky-100" : "border-red-300 focus-within:ring-red-100")}>
                        <SearchIcon aria-hidden className="size-3.5 text-slate-400" />
                        <input
                            ref={keywordsRef}
                            data-search-input=""
                            aria-label="Keywords"
                            value={state.keywords}
                            onChange={(e) => dispatch({ type: "keywords", keywords: e.target.value })}
                            onKeyDown={(e) => e.key === "Escape" && e.currentTarget.blur()}
                            placeholder={state.url ? "Running the pasted URL exactly" : `Search ${catalog?.label ?? "LinkedIn"}: keywords, or Boolean like (CEO OR founder) NOT agency`}
                            disabled={!!state.url}
                            className="h-full min-w-0 flex-1 bg-transparent text-[16px] md:text-[12.5px] text-slate-900 outline-none placeholder:text-slate-400"
                        />
                        {catalog?.boolean_keywords && catalog.keywords_hint && (
                            <Tip content={catalog.keywords_hint}>
                                <span tabIndex={0} aria-label="Boolean search help" className="inline-flex text-slate-400 outline-none">
                                    <InfoIcon className="size-3.5" />
                                </span>
                            </Tip>
                        )}
                    </div>
                    {kw.message && <p className={cn("text-[11.5px]", kw.ok ? "text-slate-500" : "text-red-600")}>{kw.message}</p>}
                </div>
                {href ? (
                    <a
                        href={href}
                        target="_blank"
                        rel="noreferrer noopener"
                        className="h-7 px-2 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex shrink-0 items-center gap-1.5"
                    >
                        <ExternalLinkIcon className="size-3.5" /> <span className="max-md:hidden">Open in LinkedIn</span>
                    </a>
                ) : (
                    <span
                        title={state.source === "recruiter_people" ? "LinkedIn keeps Recruiter filters on its side, so there is no link to rebuild." : "Run a search first"}
                        className="h-7 px-2 rounded-md text-[12px] text-slate-300 inline-flex shrink-0 items-center gap-1.5 cursor-not-allowed"
                    >
                        <ExternalLinkIcon className="size-3.5" /> <span className="max-md:hidden">Open in LinkedIn</span>
                    </span>
                )}
            </div>
            {state.url && (
                <div className="flex flex-wrap items-center gap-2 rounded-md bg-sky-50 px-3 py-1.5 text-[12px] text-sky-800">
                    <LinkIcon className="size-3.5" />
                    <span className="min-w-0 flex-1">Running the pasted LinkedIn search exactly as LinkedIn has it.</span>
                    {state.source !== "recruiter_people" && (
                        <button type="button" onClick={() => dispatch({ type: "useFilters" })} className="h-6 px-2 rounded text-[11.5px] font-medium hover:bg-sky-100">
                            Use the filters instead
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}
