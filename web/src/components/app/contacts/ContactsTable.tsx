// Contacts browser — brae-density rewrite.
//
// Visible chrome: PageTopbar > StatStrip > SectionBar > scroll body.
// Body is a dense table where each row is h-11, hairline divider, hover
// row reveals quick actions. Selecting rows pops a footer action bar.
//
// Works in two contexts:
//   - /app/contacts → full standalone browser.
//   - /app/campaigns/[id]/leads → scoped to a single campaign; the
//     parent passes `current_campaign` and the topbar collapses to a
//     section header so it nests cleanly under the campaign view.

import React from "react";
import {
    AlertTriangleIcon,
    Building2Icon,
    CheckIcon,
    DownloadIcon,
    Loader2Icon,
    MailIcon,
    MoreHorizontalIcon,
    PhoneIcon,
    PlusIcon,
    RefreshCcwIcon,
    Settings2Icon,
    TrashIcon,
    UploadIcon,
    UserPlusIcon,
} from "lucide-react";

import { useConfirm } from "@/hooks/context/confirm";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import useDeleteContacts from "@/lib/api/hooks/app/contacts/useDeleteContacts";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import ContactFilters from "./ContactFilters";
import ContactEdit from "./ContactEdit";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import ContactsEditBulk from "./ContactsEditBulk";
import { NewContactDialog } from "./NewContactDialog";

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
import { SearchInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuSeparator,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";

type SubFilter = "all" | "subscribed" | "unsubscribed";

export default function ContactsTable({
    current_campaign,
}: {
    current_campaign?: MiniCampaign;
}) {
    const confirm = useConfirm();
    const [selected, setSelected] = React.useState<string[]>([]);
    const [del, setDelete] = React.useState<boolean>(false);
    const [filtersOpen, setFiltersOpen] = React.useState<boolean>(false);
    const [edit, setEdit] = React.useState<string>("");
    const [bulkEdit, setBulkEdit] = React.useState<boolean>(false);
    const [subFilter, setSubFilter] = React.useState<SubFilter>("all");
    const [newOpen, setNewOpen] = React.useState<boolean>(false);

    function exportContacts() {
        if (!contacts || contacts.length === 0) {
            toast.error("Nothing to export yet");
            return;
        }
        const cols = ["email", "first_name", "last_name", "company", "phone", "subscribed"];
        const escape = (v: unknown) => {
            const s = v == null ? "" : String(v);
            return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
        };
        const rows = [cols.join(",")];
        for (const c of contacts) {
            rows.push(cols.map((k) => escape((c as unknown as Record<string, unknown>)[k])).join(","));
        }
        const blob = new Blob([rows.join("\n")], { type: "text/csv;charset=utf-8" });
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = `contacts-${new Date().toISOString().slice(0, 10)}.csv`;
        document.body.appendChild(a);
        a.click();
        a.remove();
        URL.revokeObjectURL(url);
        toast.success(`Exported ${contacts.length} contacts`);
    }

    function importCSV() {
        // Import flow is not yet built — surface a clear message instead
        // of silently doing nothing. Once a real importer lands this
        // becomes the trigger for the import wizard.
        toast("CSV import is coming soon — add contacts one by one for now.", {
            icon: "📄",
        });
    }

    const [searchProps, setSearchProps] = React.useState<SearchContacts>({
        query: "",
        filters: [],
        campaign_ids: current_campaign ? [current_campaign.id] : [],
        sort_by: "created_at",
        reverse: false,
    });
    const contactsData = useSearchContacts({ options: searchProps });
    const contactsBulkDelete = useDeleteContacts();

    const contacts = contactsData.contacts;
    const total = contactsData.data?.pages[0]?.pagination.total ?? 0;
    const filtered = React.useMemo(() => {
        if (!contacts) return [];
        if (subFilter === "all") return contacts;
        return contacts.filter((c) =>
            subFilter === "subscribed" ? c.subscribed : !c.subscribed,
        );
    }, [contacts, subFilter]);

    const counts = React.useMemo(() => {
        const stats = { total: contacts?.length ?? 0, subscribed: 0, unsubscribed: 0, inCampaign: 0 };
        for (const c of contacts ?? []) {
            if (c.subscribed) stats.subscribed++;
            else stats.unsubscribed++;
            if (c.campaigns && c.campaigns.length > 0) stats.inCampaign++;
        }
        return stats;
    }, [contacts]);

    const isSelectedAll = React.useMemo(() => {
        if (!filtered.length) return false;
        return filtered.every((v) => selected.includes(v.id));
    }, [filtered, selected]);

    function toggleAll() {
        if (isSelectedAll) {
            setSelected((bef) => bef.filter((id) => !filtered.some((c) => c.id === id)));
        } else {
            setSelected((bef) => Array.from(new Set([...bef, ...filtered.map((c) => c.id)])));
        }
    }

    async function bulkDelete() {
        if (selected.length === 0) return;
        try {
            confirm?.setLoading(true);
            const ids = selected;
            try {
                setDelete(true);
                await toast.promise(contactsBulkDelete.mutateAsync(ids), {
                    loading: `Deleting ${ids.length} contacts…`,
                    success: "Contacts deleted",
                    error: (err: AppError) => buildError(err),
                });
                setSelected([]);
            } finally {
                setDelete(false);
            }
        } finally {
            confirm?.setLoading(false);
            confirm?.setShow(false);
        }
    }

    const embedded = !!current_campaign;
    const tableNode = (
        <ContactsTableBody
            isLoading={contactsData.isPending}
            isError={contactsData.isError}
            errorMessage={(contactsData.error as Error | undefined)?.message ?? "Request failed."}
            onRetry={() => contactsData.refetch()}
            isRefetching={contactsData.isFetching && !contactsData.isPending}
            contacts={filtered}
            selected={selected}
            onToggle={(id, on) =>
                setSelected((bef) => (on ? [...bef, id] : bef.filter((x) => x !== id)))
            }
            isSelectedAll={isSelectedAll}
            onToggleAll={toggleAll}
            onRowClick={setEdit}
            onDelete={(id) =>
                confirm?.show(`Delete this contact?`, async () => {
                    setSelected([id]);
                    await bulkDelete();
                })
            }
            emptyTitle={
                subFilter !== "all"
                    ? `No ${subFilter} contacts`
                    : current_campaign
                        ? "No contacts in this campaign"
                        : "No contacts yet"
            }
            emptyBody={
                subFilter !== "all"
                    ? "Switch to All to see the full list."
                    : "Add or upload contacts to get started."
            }
            emptyCta={
                subFilter !== "all" ? (
                    <TopbarAction variant="ghost" onClick={() => setSubFilter("all")}>
                        Show all
                    </TopbarAction>
                ) : (
                    <TopbarAction
                        icon={<UserPlusIcon className="w-3 h-3" />}
                        onClick={() => setNewOpen(true)}
                    >
                        New contact
                    </TopbarAction>
                )
            }
            hasNextPage={!!contactsData.hasNextPage}
            isFetchingNextPage={contactsData.isFetchingNextPage}
            onLoadMore={() => contactsData.fetchNextPage()}
        />
    );

    if (embedded) {
        return (
            <>
                <SectionBar label="Leads" count={total}>
                    <SearchInput
                        value={searchProps.query}
                        onChange={(v) => setSearchProps((s) => ({ ...s, query: v }))}
                        placeholder="Search leads…"
                        className="w-56"
                    />
                    <TopbarAction
                        variant="ghost"
                        icon={<Settings2Icon className="w-3 h-3" />}
                        onClick={() => setFiltersOpen(true)}
                    >
                        Filters
                    </TopbarAction>
                    <TopbarAction
                        icon={<UserPlusIcon className="w-3 h-3" />}
                        onClick={() => setNewOpen(true)}
                    >
                        Add lead
                    </TopbarAction>
                </SectionBar>
                <div className="relative">
                    {tableNode}
                    <SelectionBar
                        count={selected.length}
                        deleting={del}
                        onBulkEdit={() => setBulkEdit(true)}
                        onDelete={() =>
                            confirm?.show(
                                `Are you sure you want to delete ${selected.length} contacts?`,
                                bulkDelete,
                            )
                        }
                        onClear={() => setSelected([])}
                    />
                </div>
                <ContactFilters
                    active={filtersOpen}
                    setActive={setFiltersOpen}
                    filters={searchProps}
                    setFilters={setSearchProps}
                    activeCampaign={current_campaign}
                    loading={contactsData.isLoading}
                />
                <ContactEdit
                    contacts={contacts ?? []}
                    active={edit}
                    setActive={setEdit}
                />
                <ContactsEditBulk active={bulkEdit} setActive={setBulkEdit} selected={selected} />
                <NewContactDialog open={newOpen} onClose={() => setNewOpen(false)} />
            </>
        );
    }

    return (
        <Page>
            <PageTopbar
                eyebrow="Contacts"
                subtitle={
                    contactsData.isPending
                        ? "Loading…"
                        : contactsData.isError
                            ? "Failed to load"
                            : `${total.toLocaleString()} total`
                }
            >
                <TopbarAction
                    variant="ghost"
                    icon={<UploadIcon className="w-3 h-3" />}
                    onClick={importCSV}
                >
                    Import CSV
                </TopbarAction>
                <TopbarAction
                    variant="ghost"
                    icon={<DownloadIcon className="w-3 h-3" />}
                    onClick={exportContacts}
                >
                    Export
                </TopbarAction>
                <TopbarAction
                    icon={<UserPlusIcon className="w-3 h-3" />}
                    onClick={() => setNewOpen(true)}
                >
                    New contact
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat
                    label="All"
                    value={counts.total}
                    sub="on this page"
                    onClick={() => setSubFilter("all")}
                />
                <Stat
                    label="Subscribed"
                    value={counts.subscribed}
                    sub="receiving mail"
                    accent={counts.subscribed > 0}
                    onClick={() => setSubFilter("subscribed")}
                />
                <Stat
                    label="Unsubscribed"
                    value={counts.unsubscribed}
                    sub="suppressed"
                    onClick={() => setSubFilter("unsubscribed")}
                />
                <Stat
                    label="In campaigns"
                    value={counts.inCampaign}
                    sub="active touchpoints"
                    last
                />
            </StatStrip>

            <SectionBar
                label={subFilter === "all" ? "All contacts" : `${subFilter[0].toUpperCase()}${subFilter.slice(1)}`}
                count={filtered.length}
            >
                <SearchInput
                    value={searchProps.query}
                    onChange={(v) => setSearchProps((s) => ({ ...s, query: v }))}
                    placeholder="Search by name, email, company…"
                    className="w-72"
                />
                <PopoverMenu align="end">
                    <PopoverMenuTrigger asChild>
                        <SelectButton
                            icon={<Settings2Icon className="w-3.5 h-3.5" />}
                            label="Sort"
                        />
                    </PopoverMenuTrigger>
                    <PopoverMenuContent>
                        <PopoverMenuLabel>Sort by</PopoverMenuLabel>
                        {[
                            ["created_at", "Date added"],
                            ["email", "Email"],
                            ["first_name", "First name"],
                            ["last_name", "Last name"],
                            ["company", "Company"],
                        ].map(([key, label]) => (
                            <PopoverMenuItem
                                key={key}
                                selected={searchProps.sort_by === key}
                                onSelect={() =>
                                    setSearchProps((s) => ({
                                        ...s,
                                        sort_by: key as SearchContacts["sort_by"],
                                    }))
                                }
                            >
                                {label}
                            </PopoverMenuItem>
                        ))}
                        <PopoverMenuSeparator />
                        <PopoverMenuItem
                            selected={searchProps.reverse}
                            onSelect={() => setSearchProps((s) => ({ ...s, reverse: !s.reverse }))}
                            closeOnSelect={false}
                        >
                            Reverse order
                        </PopoverMenuItem>
                    </PopoverMenuContent>
                </PopoverMenu>

                <TopbarAction
                    variant="ghost"
                    icon={<Settings2Icon className="w-3 h-3" />}
                    onClick={() => setFiltersOpen(true)}
                >
                    Filters
                    {searchProps.filters.length > 0 && (
                        <span className="ml-1 font-mono text-[10px] text-sky-600 tabular-nums">
                            {searchProps.filters.length}
                        </span>
                    )}
                </TopbarAction>
            </SectionBar>

            <PageBody>
                {tableNode}
            </PageBody>

            <SelectionBar
                count={selected.length}
                deleting={del}
                onBulkEdit={() => setBulkEdit(true)}
                onDelete={() =>
                    confirm?.show(
                        `Are you sure you want to delete ${selected.length} contacts?`,
                        bulkDelete,
                    )
                }
                onClear={() => setSelected([])}
            />

            {filtered.length === 0 && !contactsData.isPending ? null : null}

            <ContactFilters
                active={filtersOpen}
                setActive={setFiltersOpen}
                filters={searchProps}
                setFilters={setSearchProps}
                activeCampaign={current_campaign}
                loading={contactsData.isLoading}
            />
            <ContactEdit contacts={contacts ?? []} active={edit} setActive={setEdit} />
            <ContactsEditBulk active={bulkEdit} setActive={setBulkEdit} selected={selected} />
            <NewContactDialog open={newOpen} onClose={() => setNewOpen(false)} />
        </Page>
    );
}

function ContactsTableBody({
    isLoading,
    isError,
    errorMessage,
    onRetry,
    isRefetching,
    contacts,
    selected,
    onToggle,
    isSelectedAll,
    onToggleAll,
    onRowClick,
    onDelete,
    emptyTitle,
    emptyBody,
    emptyCta,
    hasNextPage,
    isFetchingNextPage,
    onLoadMore,
}: {
    isLoading: boolean;
    isError: boolean;
    errorMessage: string;
    onRetry: () => void;
    isRefetching: boolean;
    contacts: {
        id: string;
        first_name: string;
        last_name: string;
        email: string;
        company: string;
        phone: string;
        subscribed: boolean;
        campaigns: { id: string }[];
        created_at: Date;
    }[];
    selected: string[];
    onToggle: (id: string, on: boolean) => void;
    isSelectedAll: boolean;
    onToggleAll: () => void;
    onRowClick: (id: string) => void;
    onDelete: (id: string) => void;
    emptyTitle: string;
    emptyBody: string;
    emptyCta: React.ReactNode;
    hasNextPage: boolean;
    isFetchingNextPage: boolean;
    onLoadMore: () => void;
}) {
    if (isLoading) {
        return (
            <div className="divide-y divide-slate-200/60">
                {Array.from({ length: 10 }).map((_, i) => (
                    <div key={i} className="h-11 px-5 flex items-center gap-3">
                        <div className="w-3.5 h-3.5 bg-slate-100 rounded" />
                        <div className="w-6 h-6 rounded-full bg-slate-100 shrink-0" />
                        <div className="h-3 w-40 bg-slate-100 rounded animate-pulse" />
                        <div className="h-3 w-32 bg-slate-100 rounded animate-pulse ml-6" />
                        <div className="ml-auto h-3 w-16 bg-slate-100 rounded animate-pulse" />
                    </div>
                ))}
            </div>
        );
    }
    if (isError) {
        return (
            <div className="px-5 py-12 text-center">
                <div className="mx-auto mb-3 size-8 rounded-md bg-red-50 text-red-600 flex items-center justify-center">
                    <AlertTriangleIcon className="w-4 h-4" />
                </div>
                <p className="text-[12.5px] text-slate-900 font-medium">Couldn't load contacts</p>
                <p className="text-[11.5px] text-slate-500 mt-1 max-w-[44ch] mx-auto leading-relaxed">
                    {errorMessage}
                </p>
                <div className="mt-4 flex items-center justify-center gap-1.5">
                    <button
                        type="button"
                        onClick={onRetry}
                        disabled={isRefetching}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {isRefetching ? (
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                        ) : (
                            <RefreshCcwIcon className="w-3 h-3" />
                        )}
                        Try again
                    </button>
                    <button
                        type="button"
                        onClick={() => window.location.reload()}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-slate-700 hover:text-slate-900 text-[12px] font-medium transition-colors"
                    >
                        Reload page
                    </button>
                </div>
            </div>
        );
    }
    if (contacts.length === 0) {
        return <EmptyBlock title={emptyTitle} body={emptyBody} cta={emptyCta} />;
    }
    return (
        <>
            <table className="w-full text-left">
                <thead className="sticky top-0 bg-white z-[1]">
                    <tr className="border-b border-slate-200">
                        <th className="pl-5 pr-2 py-2 w-9">
                            <input
                                type="checkbox"
                                className="w-3.5 h-3.5 rounded accent-sky-600"
                                checked={isSelectedAll}
                                onChange={onToggleAll}
                            />
                        </th>
                        <Th>Name</Th>
                        <Th className="hidden md:table-cell">Company</Th>
                        <Th className="hidden lg:table-cell">Phone</Th>
                        <Th className="w-28">Status</Th>
                        <Th className="w-24 text-right">Campaigns</Th>
                        <Th className="w-24 text-right hidden md:table-cell">Added</Th>
                        <th className="px-3 py-2 w-12"></th>
                    </tr>
                </thead>
                <tbody>
                    {contacts.map((c) => {
                        const isSel = selected.includes(c.id);
                        const name =
                            (c.first_name || c.last_name)
                                ? `${c.first_name ?? ""} ${c.last_name ?? ""}`.trim()
                                : c.email;
                        return (
                            <tr
                                key={c.id}
                                onClick={() => onRowClick(c.id)}
                                className={`group h-11 transition-colors cursor-pointer border-b border-slate-200/60 ${
                                    isSel ? "bg-sky-50/60" : "hover:bg-slate-50/80"
                                }`}
                            >
                                <td
                                    className="pl-5 pr-2"
                                    onClick={(e) => e.stopPropagation()}
                                >
                                    <input
                                        type="checkbox"
                                        className="w-3.5 h-3.5 rounded accent-sky-600"
                                        checked={isSel}
                                        onChange={() => onToggle(c.id, !isSel)}
                                    />
                                </td>
                                <td className="px-3">
                                    <div className="flex items-center gap-2.5 min-w-0">
                                        <div className="w-6 h-6 rounded-full bg-slate-100 flex items-center justify-center shrink-0">
                                            <span className="text-[9.5px] font-semibold text-slate-600">
                                                {(c.first_name || c.email)?.slice(0, 2).toUpperCase()}
                                            </span>
                                        </div>
                                        <div className="min-w-0">
                                            <div className="text-[12.5px] text-slate-900 font-medium truncate leading-tight">
                                                {name}
                                            </div>
                                            <div className="text-[10.5px] text-slate-400 truncate font-mono leading-tight flex items-center gap-1">
                                                <MailIcon className="w-2.5 h-2.5" />
                                                {c.email}
                                            </div>
                                        </div>
                                    </div>
                                </td>
                                <td className="px-3 text-[12px] text-slate-600 truncate hidden md:table-cell">
                                    {c.company ? (
                                        <span className="inline-flex items-center gap-1.5">
                                            <Building2Icon className="w-3 h-3 text-slate-400" />
                                            {c.company}
                                        </span>
                                    ) : (
                                        <span className="text-slate-300">—</span>
                                    )}
                                </td>
                                <td className="px-3 text-[12px] text-slate-600 truncate hidden lg:table-cell font-mono">
                                    {c.phone ? (
                                        <span className="inline-flex items-center gap-1.5">
                                            <PhoneIcon className="w-3 h-3 text-slate-400" />
                                            {c.phone}
                                        </span>
                                    ) : (
                                        <span className="text-slate-300">—</span>
                                    )}
                                </td>
                                <td className="px-3">
                                    <StatusPill subscribed={c.subscribed} />
                                </td>
                                <td className="px-3 text-right font-mono text-[12px] text-slate-600 tabular-nums">
                                    {c.campaigns?.length ?? 0}
                                </td>
                                <td className="px-3 text-right font-mono text-[11px] text-slate-500 tabular-nums hidden md:table-cell">
                                    {c.created_at
                                        ? new Date(c.created_at).toLocaleDateString("en-US", {
                                              month: "short",
                                              day: "numeric",
                                          })
                                        : "—"}
                                </td>
                                <td className="px-3" onClick={(e) => e.stopPropagation()}>
                                    <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                                        <button
                                            type="button"
                                            aria-label="Delete contact"
                                            onClick={() => onDelete(c.id)}
                                            className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 flex items-center justify-center transition-colors"
                                        >
                                            <TrashIcon className="w-3 h-3" />
                                        </button>
                                        <button
                                            type="button"
                                            aria-label="More"
                                            onClick={() => onRowClick(c.id)}
                                            className="size-6 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 flex items-center justify-center transition-colors"
                                        >
                                            <MoreHorizontalIcon className="w-3 h-3" />
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        );
                    })}
                </tbody>
            </table>
            {hasNextPage && (
                <div className="px-5 py-3 flex justify-center border-t border-slate-200/60">
                    <button
                        onClick={onLoadMore}
                        disabled={isFetchingNextPage}
                        className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                    >
                        {isFetchingNextPage ? (
                            <>
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                                Loading…
                            </>
                        ) : (
                            <>
                                <PlusIcon className="w-3 h-3" />
                                Load more
                            </>
                        )}
                    </button>
                </div>
            )}
        </>
    );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <th
            className={`px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] ${className ?? ""}`}
        >
            {children}
        </th>
    );
}

function StatusPill({ subscribed }: { subscribed: boolean }) {
    if (subscribed) {
        return (
            <span className="inline-flex items-center gap-1 text-[10.5px] font-medium text-emerald-700 uppercase tracking-[0.08em]">
                <span className="size-1.5 rounded-full bg-emerald-500" />
                subscribed
            </span>
        );
    }
    return (
        <span className="inline-flex items-center gap-1 text-[10.5px] font-medium text-slate-500 uppercase tracking-[0.08em]">
            <span className="size-1.5 rounded-full bg-slate-300" />
            unsubscribed
        </span>
    );
}

function SelectionBar({
    count,
    deleting,
    onBulkEdit,
    onDelete,
    onClear,
}: {
    count: number;
    deleting: boolean;
    onBulkEdit: () => void;
    onDelete: () => void;
    onClear: () => void;
}) {
    if (count === 0) return null;
    return (
        <div className="absolute bottom-3 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5">
            <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                <CheckIcon className="w-3 h-3" />
                <span>{count} selected</span>
            </div>
            <button
                type="button"
                onClick={onBulkEdit}
                className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 font-medium transition-colors"
            >
                Edit
            </button>
            <button
                type="button"
                onClick={onDelete}
                disabled={deleting}
                className="h-7 px-2.5 rounded text-[12px] text-red-600 hover:text-white hover:bg-red-600 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
            >
                {deleting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <TrashIcon className="w-3 h-3" />}
                Delete
            </button>
            <div className="h-4 w-px bg-slate-200" />
            <button
                type="button"
                onClick={onClear}
                className="h-7 px-2.5 rounded text-[12px] text-slate-500 hover:text-slate-900 transition-colors"
            >
                Clear
            </button>
        </div>
    );
}
