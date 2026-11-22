// The three result views: a table row per person, a LinkedIn-like card, a company row and the "By company" header.
import { BriefcaseIcon, CheckIcon, ChevronDownIcon, ExternalLinkIcon, UsersIcon } from "lucide-react";
import type { MouseEvent } from "react";
import { Avatar } from "@/components/app/social/parts";
import { profileUrl } from "@/lib/social/channels";
import { Checkbox } from "@/components/ui/checkbox";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import type { FinderHit } from "@/lib/api/models/app/social/Social";
import { cn } from "@/lib/utils";
import { Badge, OutreachBadge, ReachMeter, SignalIcons, signalsOf, Spotlight, Tip, TONE, WarmblyMarks } from "./bits";
import { degreeLabel, tenure } from "./state";
import { COLUMN_LABELS, coverageLine, companyContacts, NO_COMPANY, type CompanyGroup, type TableColumn } from "./view";

/** Clicks on a row's own controls do not open the preview. */
export function rowClick(open: (() => void) | undefined) {
    return (e: MouseEvent<HTMLElement>) => {
        if (!open) return;
        if ((e.target as HTMLElement).closest("a,button,input,label,[role=checkbox],[data-no-preview]")) return;
        open();
    };
}

const WIDTH: Record<TableColumn, string> = {
    title: "minmax(0,1.3fr)",
    company: "minmax(0,1fr)",
    location: "minmax(0,0.9fr)",
    tenure: "minmax(0,0.6fr)",
    signals: "minmax(0,0.8fr)",
    reach: "88px",
    outreach: "minmax(0,0.85fr)",
    industry: "minmax(0,0.9fr)",
};

/** Checkbox, person, then the chosen columns: one template so header and rows line up. */
export function gridTemplate(columns: TableColumn[]): string {
    return ["20px", "minmax(0,1.5fr)", ...columns.map((c) => WIDTH[c])].join(" ");
}

export function TableHeader({ columns }: { columns: TableColumn[] }) {
    return (
        <div aria-hidden className="grid items-center gap-x-3 border-t border-slate-200 bg-slate-50/80 px-4 py-1.5 text-[10px] font-medium tracking-[0.14em] text-slate-400 uppercase" style={{ gridTemplateColumns: gridTemplate(columns) }}>
            <span />
            <span>Person</span>
            {columns.map((c) => (
                <span key={c} className="truncate">
                    {COLUMN_LABELS[c]}
                </span>
            ))}
        </div>
    );
}

const Muted = () => <span className="text-slate-300">-</span>;

function Cell({ column, hit }: { column: TableColumn; hit: FinderHit }) {
    const ins = hit.insights;
    const p = hit.person!;
    switch (column) {
        case "title":
            return (
                <span className="truncate" title={ins?.current_title || p.headline}>
                    {ins?.current_title || p.headline || <Muted />}
                </span>
            );
        case "company": {
            const n = companyContacts(hit);
            return (
                <span className="flex min-w-0 items-center gap-1.5">
                    <span className="truncate">{ins?.current_company || <Muted />}</span>
                    {n > 0 && (
                        <Tip content={coverageLine(n)}>
                            <span tabIndex={0} aria-label={coverageLine(n)} className="size-1.5 shrink-0 rounded-full bg-sky-500 outline-none" />
                        </Tip>
                    )}
                </span>
            );
        }
        case "location":
            return <span className="truncate">{p.location || <Muted />}</span>;
        case "tenure": {
            const t = tenure(ins?.position_started_on);
            return <span className="truncate tabular-nums">{t ?? <Muted />}</span>;
        }
        case "signals":
            return <SignalIcons hit={hit} />;
        case "reach":
            return <ReachMeter reach={hit.reach} />;
        case "outreach":
            return <OutreachBadge hit={hit} always />;
        case "industry":
            return <span className="truncate">{ins?.industry || <Muted />}</span>;
    }
}

export interface RowProps {
    hit: FinderHit;
    selected: boolean;
    onToggle: () => void;
    disabled?: boolean;
    added?: boolean;
    active?: boolean;
    onPreview?: () => void;
    onOpenContact?: (id: string) => void;
}

function ProfileLink({ url, name, className }: { url?: string; name: string; className?: string }) {
    const href = profileUrl("linkedin", url);
    if (!href) return null;
    return (
        <a href={href} target="_blank" rel="noreferrer noopener" aria-label={`Open ${name} on LinkedIn`} onClick={(e) => e.stopPropagation()} className={cn("shrink-0 text-slate-400 hover:text-slate-700", className)}>
            <ExternalLinkIcon className="size-3" />
        </a>
    );
}

export function TableRow({ hit, columns, selected, onToggle, disabled, added, active, onPreview, onOpenContact, indent }: RowProps & { columns: TableColumn[]; indent?: boolean }) {
    const p = hit.person;
    if (!p) return null;
    const degree = degreeLabel(p.network_distance);
    return (
        <div
            data-result={hit.key}
            tabIndex={-1}
            onClick={rowClick(onPreview)}
            className={cn(
                "group grid min-h-[52px] cursor-pointer items-center gap-x-3 border-b border-slate-100 px-4 py-2 text-[12.5px] outline-none",
                selected ? "bg-sky-50/60" : "hover:bg-slate-50/80",
                active && "shadow-[inset_3px_0_0_var(--color-sky-600)]",
                indent && "pl-7",
            )}
            style={{ gridTemplateColumns: gridTemplate(columns) }}
        >
            <Checkbox checked={selected} onChange={onToggle} disabled={disabled} aria-label={`Select ${p.full_name}`} onClick={(e) => e.stopPropagation()} />
            <div className="flex min-w-0 items-center gap-2.5">
                <Avatar src={p.picture_url} name={p.full_name || "LinkedIn member"} size={28} />
                <div className="grid min-w-0 gap-0.5">
                    <span className="flex min-w-0 items-center gap-1.5">
                        <span className="truncate text-[12.5px] font-medium text-slate-900">{p.full_name || "LinkedIn member"}</span>
                        {degree && <span className="shrink-0 text-[11px] text-slate-500">{degree}</span>}
                        {p.open_profile && (
                            <Tip content="Open Profile: InMail without a credit">
                                <span className="shrink-0 rounded bg-sky-50 px-1 text-[10px] font-semibold text-sky-700">OP</span>
                            </Tip>
                        )}
                        <ProfileLink url={p.profile_url} name={p.full_name} className="md:hidden md:group-hover:inline-flex" />
                    </span>
                    <span className="flex min-w-0 items-center gap-1 empty:hidden">
                        <WarmblyMarks hit={hit} added={added} onOpenContact={onOpenContact} />
                    </span>
                </div>
            </div>
            {columns.map((c) => (
                <div key={c} className="flex min-w-0 items-center text-slate-700">
                    <Cell column={c} hit={hit} />
                </div>
            ))}
        </div>
    );
}

export function ResultCard({ hit, selected, onToggle, disabled, added, active, onPreview, onOpenContact, onFindLeads }: RowProps & { onFindLeads?: (companyId: string, name: string) => void }) {
    if (hit.company) return <CompanyRow hit={hit} selected={selected} onToggle={onToggle} disabled={disabled} added={added} active={active} onFindLeads={onFindLeads} />;
    const p = hit.person;
    if (!p) return null;
    const ins = hit.insights;
    const degree = degreeLabel(p.network_distance);
    const headline = p.headline ?? "";
    const fullRole = ins?.current_title ? (ins.current_company ? `${ins.current_title} at ${ins.current_company}` : ins.current_title) : null;
    // When the headline already says the role, the role line shows only tenure.
    const role = fullRole && fullRole.toLowerCase() !== headline.toLowerCase() ? fullRole : null;
    const inRole = tenure(ins?.position_started_on);
    const atCompany = tenure(ins?.company_started_on);
    return (
        <div
            data-result={hit.key}
            tabIndex={-1}
            onClick={rowClick(onPreview)}
            className={cn(
                "group relative grid cursor-pointer grid-cols-[auto_auto_1fr] gap-x-3 border-b border-slate-100 px-4 py-3 outline-none",
                selected ? "bg-sky-50/60" : "hover:bg-slate-50/80",
                active && "shadow-[inset_3px_0_0_var(--color-sky-600)]",
            )}
        >
            <div className="pt-1">
                <Checkbox checked={selected} onChange={onToggle} disabled={disabled} aria-label={`Select ${p.full_name}`} onClick={(e) => e.stopPropagation()} />
            </div>
            <Avatar src={p.picture_url} name={p.full_name || "LinkedIn member"} size={40} />
            <div className="grid min-w-0 gap-0.5">
                <div className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1">
                    <span className="truncate text-[13px] font-semibold text-slate-900">{p.full_name || "LinkedIn member"}</span>
                    {degree && <span className="text-[12px] text-slate-500">· {degree}</span>}
                    {p.premium && <Badge tone={TONE.amber}>Premium</Badge>}
                    {p.open_profile && (
                        <Tip content="Open Profile: you can send an InMail without spending a credit.">
                            <span className="inline-flex">
                                <Badge tone={TONE.sky}>Open Profile</Badge>
                            </span>
                        </Tip>
                    )}
                    <ProfileLink url={p.profile_url} name={p.full_name} />
                </div>
                {headline && <p className="line-clamp-2 text-[12.5px] leading-snug text-slate-700">{headline}</p>}
                {(role || inRole) && (
                    <p className="flex min-w-0 flex-wrap items-center gap-x-1.5 text-[12px] text-slate-500">
                        <BriefcaseIcon aria-hidden className="size-3.5 shrink-0" />
                        {role && <span className="truncate">{role}</span>}
                        {inRole && (
                            <span className="tabular-nums">
                                {role ? "· " : ""}
                                {inRole} in role
                            </span>
                        )}
                        {atCompany && atCompany !== inRole && <span className="hidden tabular-nums lg:inline">· {atCompany} at company</span>}
                    </p>
                )}
                <p className="text-[12px] text-slate-500">
                    {p.location}
                    {ins?.industry && <span className="hidden sm:inline"> · {ins.industry}</span>}
                </p>
                <div className="mt-1.5 flex flex-wrap items-center gap-1.5 empty:hidden">
                    {hit.reach && <ReachMeter reach={hit.reach} className="mr-1" />}
                    <OutreachBadge hit={hit} />
                    {signalsOf(hit).map((sig) => (
                        <Spotlight key={sig.key} icon={sig.icon}>
                            {sig.label}
                        </Spotlight>
                    ))}
                    {(ins?.shared_connections ?? 0) > 0 && (
                        <span className="inline-flex items-center gap-1 text-[11.5px] text-slate-500">
                            <UsersIcon aria-hidden className="size-3.5" />
                            {ins!.shared_connections} shared connection{ins!.shared_connections === 1 ? "" : "s"}
                        </span>
                    )}
                    <WarmblyMarks hit={hit} added={added} onOpenContact={onOpenContact} />
                </div>
            </div>
        </div>
    );
}

function CompanyRow({ hit, selected, onToggle, disabled, added, active, onFindLeads }: RowProps & { onFindLeads?: (companyId: string, name: string) => void }) {
    const c = hit.company!;
    const n = hit.warmbly?.company_contacts ?? 0;
    const href = profileUrl("linkedin", c.linkedin_url);
    return (
        <div
            data-result={hit.key}
            tabIndex={-1}
            className={cn("grid grid-cols-[auto_auto_1fr_auto] items-start gap-x-3 border-b border-slate-100 px-4 py-3 outline-none", selected ? "bg-sky-50/60" : "hover:bg-slate-50/80", active && "shadow-[inset_3px_0_0_var(--color-sky-600)]")}
        >
            <div className="pt-1">
                <Checkbox checked={selected} onChange={onToggle} disabled={disabled} aria-label={`Select ${c.name}`} />
            </div>
            <Avatar src={c.logo_url} name={c.name} size={40} square />
            <div className="grid min-w-0 gap-0.5">
                <div className="flex flex-wrap items-center gap-1.5">
                    {href ? (
                        <a href={href} target="_blank" rel="noreferrer noopener" className="truncate text-[13px] font-semibold text-slate-900 hover:underline">
                            {c.name}
                        </a>
                    ) : (
                        <span className="truncate text-[13px] font-semibold text-slate-900">{c.name}</span>
                    )}
                    {added && <Badge tone={TONE.emerald}>Saved</Badge>}
                </div>
                <p className="text-[12px] text-slate-500">{[c.industry, c.location, c.headcount ? `${c.headcount.toLocaleString("en-US")} employees` : ""].filter(Boolean).join(" · ")}</p>
                {n > 0 && <p className="text-[11.5px] text-sky-700 tabular-nums">{coverageLine(n)}</p>}
            </div>
            {onFindLeads && c.provider_id && (
                <button
                    type="button"
                    onClick={() => onFindLeads(c.provider_id!, c.name)}
                    className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5"
                >
                    <UsersIcon className="size-3.5" /> Find leads
                </button>
            )}
        </div>
    );
}

/** A company's header in the "By company" view: people loaded here, contacts and "select top N". */
export function CompanyGroupHeader({
    group,
    selectedCount,
    selectableCount,
    canSelect,
    onToggleGroup,
    onSelectTop,
}: {
    group: CompanyGroup;
    selectedCount: number;
    selectableCount: number;
    canSelect: boolean;
    onToggleGroup: () => void;
    onSelectTop: (n: number) => void;
}) {
    const all = selectableCount > 0 && selectedCount === selectableCount;
    const none = group.key === NO_COMPANY;
    return (
        <div className="flex min-h-[52px] items-center gap-3 border-b border-slate-200 bg-slate-50/70 px-4 py-2">
            <Checkbox
                checked={all}
                ref={(el) => {
                    if (el) el.indeterminate = !all && selectedCount > 0;
                }}
                onChange={onToggleGroup}
                disabled={!canSelect || selectableCount === 0}
                aria-label={`Select everyone loaded at ${group.name}`}
            />
            <Avatar name={group.name} size={28} square />
            <div className="grid min-w-0 flex-1 gap-0.5">
                <p className="flex min-w-0 items-center gap-2 text-[12.5px] font-semibold text-slate-900">
                    <span className="truncate">{group.name}</span>
                    <span className="shrink-0 text-[11.5px] font-normal text-slate-500 tabular-nums">
                        {group.hits.length} {group.hits.length === 1 ? "person" : "people"} here
                    </span>
                </p>
                {!none && <p className="truncate text-[11.5px] text-slate-500 tabular-nums">{coverageLine(group.contacts)}</p>}
            </div>
            {!none && canSelect && selectableCount > 1 && (
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <button type="button" className="h-7 px-2 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1">
                            <CheckIcon className="size-3.5" /> <span className="max-sm:hidden">Select top</span> <ChevronDownIcon className="size-3" />
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent minWidth={160}>
                        {[1, 2, 3, 5]
                            .filter((n) => n < selectableCount || n === 1)
                            .map((n) => (
                                <PopoverMenuItem key={n} onSelect={() => onSelectTop(n)}>
                                    Top {n} by reach
                                </PopoverMenuItem>
                            ))}
                    </PopoverMenuContent>
                </PopoverMenu>
            )}
        </div>
    );
}

export function ResultSkeletons({ rows = 6 }: { rows?: number }) {
    return (
        <div aria-hidden className="grid">
            {Array.from({ length: rows }, (_, i) => (
                <div key={i} className="grid grid-cols-[auto_auto_1fr] gap-x-3 border-b border-slate-100 px-4 py-3">
                    <span className="mt-1 size-3.5 rounded bg-slate-100 animate-pulse" />
                    <span className="size-10 rounded-full bg-slate-100 animate-pulse" />
                    <div className="grid gap-2 pt-0.5">
                        <span className="h-3 w-40 rounded bg-slate-100 animate-pulse" />
                        <span className="h-2.5 w-[min(30rem,90%)] rounded bg-slate-100 animate-pulse" />
                        <span className="h-2.5 w-56 rounded bg-slate-100 animate-pulse" />
                    </div>
                </div>
            ))}
        </div>
    );
}
