// Forms: hosted lead-capture forms (issue #267). A leads-style table (same
// visual language as campaigns > leads / ContactsTable): live counters, a
// submissions sparkline, sorting, multi-select with bulk actions; clicking a
// row opens the builder.

import React from "react";
import { useNavigate } from "react-router-dom";
import { CheckIcon, ChevronDownIcon, ChevronUpIcon, ClipboardListIcon, LinkIcon, Loader2Icon, MoreHorizontalIcon, PlusIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";

import { EmptyBlock, Page, PageBody, PageTopbar, SectionBar, Stat, StatStrip, TopbarAction } from "@/components/layout/Page";
import { NoAccess } from "@/components/layout/NoAccess";
import { SearchInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { Sparkline } from "@/components/ui/charts";
import { useUserProfile } from "@/hooks/context/user";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { usePermission, useWriteGuard } from "@/hooks/usePermission";
import { useCreateForm, useDeleteForm, useForms, useUpdateForm } from "@/lib/api/hooks/app/forms";
import type Form from "@/lib/api/models/app/forms/Form";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import timeAgo from "@/lib/helper/timeAgo";
import { Checkbox } from "@/components/ui/checkbox";

const STATUS_PILL: Record<Form["status"], string> = {
    draft: "bg-slate-100 text-slate-600",
    published: "bg-emerald-50 text-emerald-700",
    archived: "bg-amber-50 text-amber-700",
};

function conversionValue(f: Form): number {
    if (f.views_count <= 0) return -1;
    return Math.min(100, (f.submissions_count / f.views_count) * 100);
}

function conversion(f: Form): string {
    const v = conversionValue(f);
    return v < 0 ? "–" : `${v.toFixed(1)}%`;
}

type SortKey = "name" | "views" | "starts" | "submissions" | "conversion" | "identified" | "created";
type StatusFilter = "all" | Form["status"];

const STATUS_TABS: { value: StatusFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "published", label: "Published" },
    { value: "draft", label: "Drafts" },
    { value: "archived", label: "Archived" },
];

const SORT_VALUE: Record<SortKey, (f: Form) => number | string> = {
    name: (f) => f.name.toLowerCase(),
    views: (f) => f.views_count,
    starts: (f) => f.starts_count ?? 0,
    submissions: (f) => f.submissions_count,
    conversion: conversionValue,
    identified: (f) => f.identified_count ?? 0,
    created: (f) => new Date(f.created_at).getTime(),
};

export default function FormsPage() {
    const canView = usePermission("VIEW_CONTACTS");
    if (!canView) return <NoAccess feature="forms" permissionLabel="View contacts" />;
    return <FormsList />;
}

function FormsList() {
    const navigate = useNavigate();
    const confirm = useConfirm();
    const write = useWriteGuard("MANAGE_CONTACTS");
    const guarded = (fn: () => void) => () => write.guard(fn)({});
    const forms = useForms();
    const create = useCreateForm();
    const update = useUpdateForm();
    const remove = useDeleteForm();

    const { user } = useUserProfile();
    const categories = React.useMemo(() => user?.categories ?? [], [user?.categories]);
    const categoryById = React.useMemo(() => new Map(categories.map((c) => [c.id, c])), [categories]);

    const [query, setQuery] = React.useState("");
    const [status, setStatusFilter] = React.useState<StatusFilter>("all");
    const [category, setCategory] = React.useState("");
    const [sort, setSort] = React.useState<{ key: SortKey; dir: 1 | -1 }>({ key: "created", dir: -1 });
    const [selected, setSelected] = React.useState<string[]>([]);
    const [bulkBusy, setBulkBusy] = React.useState(false);

    const statusCounts = React.useMemo(() => {
        const all = forms.data ?? [];
        const counts: Record<StatusFilter, number> = { all: all.length, draft: 0, published: 0, archived: 0 };
        for (const f of all) counts[f.status] += 1;
        return counts;
    }, [forms.data]);

    const list = React.useMemo(() => {
        const all = forms.data ?? [];
        const q = query.trim().toLowerCase();
        const filtered = all.filter(
            (f) =>
                (status === "all" || f.status === status) &&
                (category === "" || (f.category_ids ?? []).includes(category)) &&
                (!q || f.name.toLowerCase().includes(q)),
        );
        const value = SORT_VALUE[sort.key];
        filtered.sort((a, b) => {
            const va = value(a);
            const vb = value(b);
            if (va === vb) return 0;
            return (va < vb ? -1 : 1) * sort.dir;
        });
        return filtered;
    }, [forms.data, query, status, category, sort]);

    const totals = React.useMemo(() => {
        const all = forms.data ?? [];
        const views = all.reduce((n, f) => n + f.views_count, 0);
        const starts = all.reduce((n, f) => n + (f.starts_count ?? 0), 0);
        const subs = all.reduce((n, f) => n + f.submissions_count, 0);
        return {
            forms: all.length,
            live: all.filter((f) => f.status === "published").length,
            views,
            starts,
            subs,
            conversion: views > 0 ? `${Math.min(100, (subs / views) * 100).toFixed(1)}%` : "–",
        };
    }, [forms.data]);

    const selectedSet = React.useMemo(() => new Set(selected), [selected]);
    const allSelected = list.length > 0 && list.every((f) => selectedSet.has(f.id));

    function toggleAll() {
        setSelected(allSelected ? [] : list.map((f) => f.id));
    }

    function toggleOne(id: string, on: boolean) {
        setSelected((prev) => (on ? [...prev, id] : prev.filter((x) => x !== id)));
    }

    function sortBy(key: SortKey) {
        setSort((prev) => (prev.key === key ? { key, dir: prev.dir === 1 ? -1 : 1 } : { key, dir: key === "name" ? 1 : -1 }));
    }

    async function createNew() {
        try {
            const f = await create.mutateAsync("Untitled form");
            navigate(`/app/forms/${f.id}`);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function duplicate(f: Form) {
        try {
            const copy = await create.mutateAsync(`${f.name} (copy)`.slice(0, 120));
            await update.mutateAsync({
                id: copy.id,
                w: {
                    fields: f.fields,
                    design: f.design,
                    success_message: f.success_message,
                    redirect_url: f.redirect_url,
                    campaign_id: f.campaign_id ?? null,
                    category_ids: f.category_ids,
                    allowed_domains: f.allowed_domains,
                    captcha_enabled: f.captcha_enabled,
                    triage_enabled: f.triage_enabled,
                },
            });
            toast.success(`Duplicated as ${copy.name}`);
            navigate(`/app/forms/${copy.id}`);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function setStatus(f: Form, status: Form["status"]) {
        try {
            await update.mutateAsync({ id: f.id, w: { status } });
            toast.success(status === "published" ? "Form published" : status === "draft" ? "Form unpublished" : "Form archived");
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    function askDelete(f: Form) {
        confirm.show(`Delete the form "${f.name}" and its ${f.submissions_count.toLocaleString()} submissions? Contacts it created are kept.`, async () => {
            try {
                await remove.mutateAsync(f.id);
                toast.success("Form deleted");
            } catch (err) {
                toast.error(buildError(err as AppError));
            }
        });
    }

    async function copyLink(f: Form) {
        if (!f.share_url) {
            toast.error("No public URL is configured for this instance");
            return;
        }
        await navigator.clipboard.writeText(f.share_url);
        toast.success("Link copied");
    }

    // Bulk actions run per-id over the existing mutations; a partial failure
    // reports how many made it.
    async function bulkStatus(status: Form["status"], label: string) {
        setBulkBusy(true);
        const results = await Promise.allSettled(selected.map((id) => update.mutateAsync({ id, w: { status } })));
        setBulkBusy(false);
        const ok = results.filter((r) => r.status === "fulfilled").length;
        if (ok === results.length) toast.success(`${ok} ${label}`);
        else toast.error(`${ok} of ${results.length} ${label}; the rest failed`);
        setSelected([]);
    }

    function bulkDelete() {
        const ids = [...selected];
        const total = (forms.data ?? []).filter((f) => ids.includes(f.id)).reduce((n, f) => n + f.submissions_count, 0);
        confirm.show(
            `Delete ${ids.length} forms and their ${total.toLocaleString()} submissions? Contacts they created are kept.`,
            async () => {
                setBulkBusy(true);
                const results = await Promise.allSettled(ids.map((id) => remove.mutateAsync(id)));
                setBulkBusy(false);
                const ok = results.filter((r) => r.status === "fulfilled").length;
                if (ok === results.length) toast.success(`${ok} forms deleted`);
                else toast.error(`${ok} of ${results.length} deleted; the rest failed`);
                setSelected([]);
            },
        );
    }

    return (
        <Page>
            <PageTopbar eyebrow="Forms" subtitle="Hosted lead-capture forms you can embed anywhere">
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={guarded(createNew)}>
                    New form
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={5}>
                <Stat label="Forms" value={totals.forms} sub={`${totals.live} live`} />
                <Stat label="Views" value={totals.views.toLocaleString()} sub="all time" />
                <Stat label="Starts" value={totals.starts.toLocaleString()} sub="began filling" />
                <Stat label="Submissions" value={totals.subs.toLocaleString()} sub="all time" accent={totals.subs > 0} />
                <Stat label="Conversion" value={totals.conversion} sub="submissions / views" last />
            </StatStrip>

            <SectionBar label="All forms" count={list.length}>
                <div className="flex flex-wrap items-center gap-2">
                    <div className="inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5">
                        {STATUS_TABS.map((t) => (
                            <button
                                key={t.value}
                                type="button"
                                onClick={() => setStatusFilter(t.value)}
                                className={`h-6 px-2 rounded text-[11.5px] font-medium transition-colors ${
                                    status === t.value
                                        ? "bg-white text-slate-900 shadow-sm"
                                        : "text-slate-500 hover:text-slate-900"
                                }`}
                            >
                                {t.label}
                                <span className={`ml-1 tabular-nums ${status === t.value ? "text-slate-400" : "text-slate-400/80"}`}>
                                    {statusCounts[t.value]}
                                </span>
                            </button>
                        ))}
                    </div>
                    {categories.length > 0 && (
                        <SelectMenu
                            value={category}
                            onChange={setCategory}
                            options={[
                                { value: "", label: "All categories" },
                                ...categories.map((c) => ({ value: c.id, label: c.title })),
                            ]}
                            aria-label="Filter by category"
                        />
                    )}
                    <SearchInput value={query} onChange={setQuery} placeholder="Search forms…" className="w-full sm:w-56" />
                </div>
            </SectionBar>

            <PageBody>
                {forms.isPending ? (
                    <div className="p-3 space-y-1.5">
                        {[...Array(4)].map((_, i) => (
                            <div key={i} className="h-11 rounded-md bg-slate-100 animate-pulse" />
                        ))}
                    </div>
                ) : forms.isError ? (
                    <EmptyBlock title="Couldn't load forms" body="Try again in a moment." />
                ) : list.length === 0 ? (
                    (() => {
                        const filtered = Boolean(query) || status !== "all" || category !== "";
                        return (
                            <EmptyBlock
                                title={filtered ? "No forms match" : "No forms yet"}
                                body={
                                    filtered
                                        ? "Try a different search or filter."
                                        : "Build a form, style it to match your site, and every submission becomes a contact — filed under your categories and optionally dropped straight into a campaign."
                                }
                                cta={
                                    filtered ? undefined : (
                                        <TopbarAction icon={<PlusIcon className="w-3 h-3" />} onClick={guarded(createNew)}>
                                            New form
                                        </TopbarAction>
                                    )
                                }
                            />
                        );
                    })()
                ) : (
                    <table className="w-full text-left">
                        <thead className="sticky top-0 bg-white z-[1]">
                            <tr className="border-b border-slate-200">
                                <th className="pl-5 pr-2 py-2 w-9">
                                    <Checkbox
                                        checked={allSelected}
                                        onChange={toggleAll}
                                    />
                                </th>
                                <SortTh label="Name" k="name" sort={sort} onSort={sortBy} className="max-w-0 w-full md:max-w-none md:w-auto" />
                                <Th className="w-40 hidden lg:table-cell">Categories</Th>
                                <Th className="w-24 hidden lg:table-cell">Trend</Th>
                                <SortTh label="Views" k="views" sort={sort} onSort={sortBy} className="w-16 text-right" right />
                                <SortTh label="Starts" k="starts" sort={sort} onSort={sortBy} className="w-16 text-right hidden md:table-cell" right />
                                <SortTh label="Subs" k="submissions" sort={sort} onSort={sortBy} className="w-16 text-right" right />
                                <SortTh label="Conv" k="conversion" sort={sort} onSort={sortBy} className="w-16 text-right hidden sm:table-cell" right />
                                <SortTh label="Identified" k="identified" sort={sort} onSort={sortBy} className="w-20 text-right hidden md:table-cell" right />
                                <SortTh label="Created" k="created" sort={sort} onSort={sortBy} className="w-24 hidden xl:table-cell" />
                                <th className="w-12" />
                            </tr>
                        </thead>
                        <tbody>
                            {list.map((f) => {
                                const isSel = selectedSet.has(f.id);
                                return (
                                    <tr
                                        key={f.id}
                                        onClick={() => navigate(`/app/forms/${f.id}`)}
                                        className={`h-11 border-b border-slate-100 cursor-pointer transition-colors ${
                                            isSel ? "bg-sky-50/60 hover:bg-sky-50/80" : "hover:bg-slate-50/80"
                                        }`}
                                    >
                                        <td className="pl-5 pr-2" onClick={(e) => e.stopPropagation()}>
                                            <Checkbox
                                                checked={isSel}
                                                onChange={(e) => toggleOne(f.id, e.target.checked)}
                                            />
                                        </td>
                                        <td className="px-3 max-w-0 w-full md:max-w-none md:w-auto">
                                            <div className="flex items-center gap-2.5 min-w-0">
                                                <div className="w-6 h-6 rounded-md bg-slate-100 flex items-center justify-center shrink-0">
                                                    <ClipboardListIcon className="w-3 h-3 text-slate-500" />
                                                </div>
                                                <div className="min-w-0">
                                                    <div className="text-[12.5px] font-medium text-slate-900 truncate leading-tight flex items-center gap-1.5">
                                                        <span className="truncate">{f.name}</span>
                                                        <span className={`inline-flex items-center h-4 px-1.5 rounded text-[10px] font-medium shrink-0 ${STATUS_PILL[f.status]}`}>
                                                            {f.status}
                                                        </span>
                                                    </div>
                                                    <div className="text-[11px] text-slate-500 truncate">
                                                        Last submission {timeAgo(f.last_submission_at)}
                                                    </div>
                                                </div>
                                            </div>
                                        </td>
                                        <td className="px-3 hidden lg:table-cell">
                                            <CategoryChips ids={f.category_ids ?? []} byId={categoryById} />
                                        </td>
                                        <td className="px-3 hidden lg:table-cell">
                                            {(f.trend ?? []).some((v) => v > 0) ? (
                                                <Sparkline values={f.trend ?? []} width={84} height={22} />
                                            ) : (
                                                <span className="text-[11px] text-slate-300">–</span>
                                            )}
                                        </td>
                                        <NumTd muted>{f.views_count.toLocaleString()}</NumTd>
                                        <NumTd muted className="hidden md:table-cell">
                                            {(f.starts_count ?? 0).toLocaleString()}
                                        </NumTd>
                                        <NumTd>{f.submissions_count.toLocaleString()}</NumTd>
                                        <NumTd muted className="hidden sm:table-cell">
                                            {conversion(f)}
                                        </NumTd>
                                        <NumTd muted className="hidden md:table-cell">
                                            {(f.identified_count ?? 0).toLocaleString()}
                                        </NumTd>
                                        <td className="px-3 text-[12px] text-slate-500 whitespace-nowrap hidden xl:table-cell">
                                            {timeAgo(f.created_at)}
                                        </td>
                                        <td className="pr-3" onClick={(e) => e.stopPropagation()}>
                                            <PopoverMenu align="end">
                                                <PopoverMenuTrigger asChild>
                                                    <button
                                                        type="button"
                                                        aria-label="More"
                                                        className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                                                    >
                                                        <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                                    </button>
                                                </PopoverMenuTrigger>
                                                <PopoverMenuContent minWidth={190}>
                                                    <PopoverMenuItem onSelect={() => navigate(`/app/forms/${f.id}`)}>Open builder</PopoverMenuItem>
                                                    <PopoverMenuItem onSelect={() => navigate(`/app/forms/${f.id}?tab=analytics`)}>
                                                        View analytics
                                                    </PopoverMenuItem>
                                                    <PopoverMenuItem onSelect={() => navigate(`/app/forms/${f.id}?tab=submissions`)}>
                                                        View submissions
                                                    </PopoverMenuItem>
                                                    {f.status === "published" && (
                                                        <PopoverMenuItem onSelect={() => void copyLink(f)}>
                                                            <span className="inline-flex items-center gap-1.5">
                                                                <LinkIcon className="w-3 h-3" /> Copy link
                                                            </span>
                                                        </PopoverMenuItem>
                                                    )}
                                                    <PopoverMenuSeparator />
                                                    {f.status !== "published" && (
                                                        <PopoverMenuItem onSelect={guarded(() => void setStatus(f, "published"))}>Publish</PopoverMenuItem>
                                                    )}
                                                    {f.status === "published" && (
                                                        <PopoverMenuItem onSelect={guarded(() => void setStatus(f, "draft"))}>Unpublish</PopoverMenuItem>
                                                    )}
                                                    {f.status !== "archived" && (
                                                        <PopoverMenuItem onSelect={guarded(() => void setStatus(f, "archived"))}>Archive</PopoverMenuItem>
                                                    )}
                                                    <PopoverMenuItem onSelect={guarded(() => void duplicate(f))}>Duplicate</PopoverMenuItem>
                                                    <PopoverMenuSeparator />
                                                    <PopoverMenuItem onSelect={guarded(() => askDelete(f))}>Delete</PopoverMenuItem>
                                                </PopoverMenuContent>
                                            </PopoverMenu>
                                        </td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                )}
            </PageBody>

            {selected.length > 0 && (
                <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 flex items-center max-w-[calc(100vw-16px)] flex-wrap justify-center md:max-w-none md:flex-nowrap gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5">
                    <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                        <CheckIcon className="w-3 h-3" />
                        <span>{selected.length} selected</span>
                    </div>
                    <BarButton disabled={bulkBusy} onClick={guarded(() => void bulkStatus("published", "published"))}>
                        Publish
                    </BarButton>
                    <BarButton disabled={bulkBusy} onClick={guarded(() => void bulkStatus("draft", "unpublished"))}>
                        Unpublish
                    </BarButton>
                    <BarButton disabled={bulkBusy} onClick={guarded(() => void bulkStatus("archived", "archived"))}>
                        Archive
                    </BarButton>
                    <BarButton danger disabled={bulkBusy} onClick={guarded(bulkDelete)}>
                        {bulkBusy ? <Loader2Icon className="w-3 h-3 animate-spin" /> : "Delete"}
                    </BarButton>
                    <button
                        type="button"
                        aria-label="Clear selection"
                        onClick={() => setSelected([])}
                        className="size-7 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
            )}
        </Page>
    );
}

// CategoryChips shows where a form files its contacts: up to two colored
// category chips plus an overflow count.
function CategoryChips({ ids, byId }: { ids: string[]; byId: Map<string, { id: string; title: string; color: string }> }) {
    const cats = ids.map((id) => byId.get(id)).filter((c): c is NonNullable<typeof c> => Boolean(c));
    if (cats.length === 0) return <span className="text-[11px] text-slate-300">–</span>;
    const shown = cats.slice(0, 2);
    return (
        <div className="flex items-center gap-1 min-w-0">
            {shown.map((c) => (
                <span
                    key={c.id}
                    className="inline-flex items-center gap-1 h-4.5 px-1.5 rounded bg-slate-50 border border-slate-200 text-[10.5px] text-slate-600 min-w-0"
                >
                    <span className="w-1.5 h-1.5 rounded-full shrink-0" style={{ backgroundColor: c.color || "#94a3b8" }} />
                    <span className="truncate max-w-20">{c.title}</span>
                </span>
            ))}
            {cats.length > 2 && <span className="text-[10.5px] text-slate-400 shrink-0">+{cats.length - 2}</span>}
        </div>
    );
}

function Th({ children, className }: { children?: React.ReactNode; className?: string }) {
    return (
        <th className={`px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] ${className ?? ""}`}>
            {children}
        </th>
    );
}

function SortTh({
    label,
    k,
    sort,
    onSort,
    className,
    right = false,
}: {
    label: string;
    k: SortKey;
    sort: { key: SortKey; dir: 1 | -1 };
    onSort: (k: SortKey) => void;
    className?: string;
    right?: boolean;
}) {
    const active = sort.key === k;
    const Arrow = sort.dir === 1 ? ChevronUpIcon : ChevronDownIcon;
    return (
        <Th className={className}>
            <button
                type="button"
                onClick={() => onSort(k)}
                className={`inline-flex items-center gap-0.5 uppercase tracking-[0.14em] transition-colors ${
                    right ? "justify-end" : ""
                } ${active ? "text-slate-700" : "hover:text-slate-600"}`}
            >
                {label}
                {active && <Arrow className="w-2.5 h-2.5" />}
            </button>
        </Th>
    );
}

function NumTd({ children, muted = false, className }: { children: React.ReactNode; muted?: boolean; className?: string }) {
    return (
        <td className={`px-3 text-right font-mono text-[12px] tabular-nums whitespace-nowrap ${muted ? "text-slate-500" : "text-slate-900"} ${className ?? ""}`}>
            {children}
        </td>
    );
}

function BarButton({
    children,
    onClick,
    disabled = false,
    danger = false,
}: {
    children: React.ReactNode;
    onClick: () => void;
    disabled?: boolean;
    danger?: boolean;
}) {
    return (
        <button
            type="button"
            disabled={disabled}
            onClick={onClick}
            className={`h-7 px-2.5 rounded text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60 ${
                danger ? "text-rose-600 hover:bg-rose-50" : "text-slate-700 hover:text-slate-900 hover:bg-slate-100"
            }`}
        >
            {children}
        </button>
    );
}
