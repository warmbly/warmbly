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
    BanIcon,
    Building2Icon,
    CableIcon,
    CheckIcon,
    ClockIcon,
    CornerUpLeftIcon,
    DownloadIcon,
    Loader2Icon,
    MailIcon,
    MoreHorizontalIcon,
    PhoneIcon,
    PlusIcon,
    RefreshCcwIcon,
    Settings2Icon,
    SheetIcon,
    SparklesIcon,
    TrashIcon,
    UploadIcon,
    UserPlusIcon,
} from "lucide-react";

import { useConfirm } from "@/hooks/context/confirm";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import useDeleteContacts from "@/lib/api/hooks/app/contacts/useDeleteContacts";
import { useBatchResearch } from "@/lib/api/hooks/app/contacts/useContactResearch";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import { usePushContacts } from "@/lib/api/hooks/app/integrations/usePushContacts";
import {
    PROVIDER_LABELS,
    PUSHABLE_PROVIDERS,
    type IntegrationConnection,
} from "@/lib/api/models/app/integrations/Integration";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import ContactFilters from "./ContactFilters";
import ContactEdit from "./ContactEdit";
import type { ContactSlideTab } from "./contact-edit/tabs";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { ContactCampaignProgress, LeadStatus } from "@/lib/api/models/app/contacts/Contact";
import ContactsEditBulk from "./ContactsEditBulk";
import { NewContactDialog } from "./NewContactDialog";
import ExportDialog from "./ExportDialog";
import ImportWizard from "./ImportWizard";
import SyncSourcesPanel from "./SyncSourcesPanel";
import { CategoryChip } from "./CategoryPicker";

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
    // Which tab the drawer opens on. Row click → default (overview); the
    // right-side 3-dots → "details", mirroring the mailbox 3-dots → settings.
    const [editTab, setEditTab] = React.useState<ContactSlideTab | undefined>(undefined);
    const openContact = React.useCallback((id: string, tab?: ContactSlideTab) => {
        setEditTab(tab);
        setEdit(id);
    }, []);
    const [bulkEdit, setBulkEdit] = React.useState<boolean>(false);
    const [subFilter, setSubFilter] = React.useState<SubFilter>("all");
    const [newOpen, setNewOpen] = React.useState<boolean>(false);
    const [exportOpen, setExportOpen] = React.useState<boolean>(false);
    const [importOpen, setImportOpen] = React.useState<boolean>(false);
    const [syncOpen, setSyncOpen] = React.useState<boolean>(false);

    const [searchProps, setSearchProps] = React.useState<SearchContacts>({
        query: "",
        filters: [],
        campaign_ids: current_campaign ? [current_campaign.id] : [],
        sort_by: "created_at",
        reverse: false,
    });
    const contactsData = useSearchContacts({ options: searchProps });
    const contactsBulkDelete = useDeleteContacts();

    // Connected CRM targets the "Push to CRM" bulk action can reach. Driven by
    // the org's live connections (backend enforces the push permission).
    const connectionsQuery = useIntegrationConnections();
    const pushContacts = usePushContacts();
    const pushTargets = React.useMemo<IntegrationConnection[]>(
        () =>
            (connectionsQuery.data?.connections ?? []).filter(
                (c) =>
                    PUSHABLE_PROVIDERS.includes(c.provider) &&
                    (c.status === "connected" || c.status === "degraded"),
            ),
        [connectionsQuery.data],
    );

    async function pushToCRM(connectionId: string, providerLabel: string) {
        if (selected.length === 0 || pushContacts.isPending) return;
        const ids = selected;
        const t = toast.loading(`Pushing ${ids.length} to ${providerLabel}…`);
        try {
            const res = await pushContacts.mutateAsync({ connectionId, contact_ids: ids });
            if (res.pushed === 0) {
                toast.error(
                    `Couldn't push to ${providerLabel}${res.failed ? ` (${res.failed} failed)` : ""}`,
                    { id: t },
                );
            } else if (res.failed > 0) {
                toast.success(`Pushed ${res.pushed} to ${providerLabel}, ${res.failed} failed`, { id: t });
            } else {
                toast.success(`Pushed ${res.pushed} to ${providerLabel}`, { id: t });
            }
        } catch (err) {
            toast.error(buildError(err as AppError), { id: t });
        }
    }

    const contacts = contactsData.contacts;
    const total = contactsData.data?.pages[0]?.pagination.total ?? 0;
    const filtered = React.useMemo(() => {
        if (!contacts) return [];
        if (subFilter === "all") return contacts;
        return contacts.filter((c) =>
            subFilter === "subscribed" ? c.subscribed : !c.subscribed,
        );
    }, [contacts, subFilter]);

    // Prefer the server's org-wide facet counts (first page's `counts` block) so
    // the stat strip is accurate at scale. Before that lands, fall back to a
    // count over the loaded rows so the strip isn't blank on first paint.
    const serverCounts = contactsData.data?.pages[0]?.counts;
    const counts = React.useMemo(() => {
        if (serverCounts) {
            return {
                total: serverCounts.total,
                subscribed: serverCounts.subscribed,
                unsubscribed: serverCounts.unsubscribed,
                inCampaign: serverCounts.in_campaign,
                exact: true,
            };
        }
        const stats = { total: contacts?.length ?? 0, subscribed: 0, unsubscribed: 0, inCampaign: 0, exact: false };
        for (const c of contacts ?? []) {
            if (c.subscribed) stats.subscribed++;
            else stats.unsubscribed++;
            if (c.campaigns && c.campaigns.length > 0) stats.inCampaign++;
        }
        return stats;
    }, [serverCounts, contacts]);

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

    // Bulk AI research. Confirms the credit cost (2 per contact) before queuing;
    // runs drain in the background and the tab refreshes live via realtime.
    const batchResearch = useBatchResearch();
    function bulkResearch() {
        if (selected.length === 0) return;
        const ids = selected;
        confirm?.show(
            `Research ${ids.length} ${ids.length === 1 ? "contact" : "contacts"}? This uses up to ${ids.length * 2} AI credits and runs in the background.`,
            async () => {
                const res = await batchResearch.mutateAsync({ contactIds: ids, objective: "" });
                toast.success(`Queued research for ${res.queued} contacts`);
                setSelected([]);
            },
        );
    }

    const embedded = !!current_campaign;
    const tableNode = (
        <ContactsTableBody
            embedded={embedded}
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
            onRowClick={openContact}
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
                        className="w-full sm:w-56"
                    />
                    <TopbarAction
                        variant="ghost"
                        icon={<Settings2Icon className="w-3 h-3" />}
                        onClick={() => setFiltersOpen(true)}
                    >
                        Filters
                    </TopbarAction>
                    <TopbarAction
                        variant="ghost"
                        icon={<UploadIcon className="w-3 h-3" />}
                        onClick={() => setImportOpen(true)}
                    >
                        Import
                    </TopbarAction>
                    <TopbarAction
                        variant="ghost"
                        icon={<SheetIcon className="w-3 h-3" />}
                        onClick={() => setSyncOpen(true)}
                    >
                        Sheet sync
                    </TopbarAction>
                    <TopbarAction
                        icon={<UserPlusIcon className="w-3 h-3" />}
                        onClick={() => setNewOpen(true)}
                    >
                        Add lead
                    </TopbarAction>
                </SectionBar>
                <LeadProgressStrip
                    contacts={contacts ?? []}
                    total={total}
                    hasMore={!!contactsData.hasNextPage}
                />
                <div className="relative">
                    {tableNode}
                    <SelectionBar
                        count={selected.length}
                        deleting={del}
                        pushTargets={pushTargets}
                        pushing={pushContacts.isPending}
                        onPush={pushToCRM}
                        onBulkEdit={() => setBulkEdit(true)}
                        onResearch={bulkResearch}
                        researching={batchResearch.isPending}
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
                <NewContactDialog open={newOpen} onClose={() => setNewOpen(false)} campaign={current_campaign} />
                <SyncSourcesPanel
                    open={syncOpen}
                    onClose={() => setSyncOpen(false)}
                    campaign={current_campaign}
                />
                <ImportWizard
                    open={importOpen}
                    onClose={() => setImportOpen(false)}
                    lockedCampaign={current_campaign}
                />
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
                <div className="hidden md:contents">
                    <TopbarAction
                        variant="ghost"
                        icon={<UploadIcon className="w-3 h-3" />}
                        onClick={() => setImportOpen(true)}
                    >
                        Import
                    </TopbarAction>
                    <TopbarAction
                        variant="ghost"
                        icon={<SheetIcon className="w-3 h-3" />}
                        onClick={() => setSyncOpen(true)}
                    >
                        Sheet sync
                    </TopbarAction>
                    <TopbarAction
                        variant="ghost"
                        icon={<DownloadIcon className="w-3 h-3" />}
                        onClick={() => setExportOpen(true)}
                    >
                        Export
                    </TopbarAction>
                </div>
                <div className="md:hidden">
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <SelectButton
                                icon={<MoreHorizontalIcon className="w-3.5 h-3.5" />}
                                aria-label="More actions"
                            />
                        </PopoverMenuTrigger>
                        <PopoverMenuContent>
                            <PopoverMenuItem onSelect={() => setImportOpen(true)}>
                                Import
                            </PopoverMenuItem>
                            <PopoverMenuItem onSelect={() => setSyncOpen(true)}>
                                Sheet sync
                            </PopoverMenuItem>
                            <PopoverMenuItem onSelect={() => setExportOpen(true)}>
                                Export
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
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
                    sub={counts.exact ? "total contacts" : "on this page"}
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
                    className="w-full sm:w-72"
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
                pushTargets={pushTargets}
                pushing={pushContacts.isPending}
                onPush={pushToCRM}
                onBulkEdit={() => setBulkEdit(true)}
                onResearch={bulkResearch}
                researching={batchResearch.isPending}
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
            <ContactEdit contacts={contacts ?? []} active={edit} setActive={setEdit} initialTab={editTab} />
            <ContactsEditBulk active={bulkEdit} setActive={setBulkEdit} selected={selected} />
            <NewContactDialog open={newOpen} onClose={() => setNewOpen(false)} />
            <ExportDialog
                open={exportOpen}
                onClose={() => setExportOpen(false)}
                filters={searchProps}
                selectedIds={selected}
                totalKnown={total}
            />
            <ImportWizard
                open={importOpen}
                onClose={() => setImportOpen(false)}
            />
            <SyncSourcesPanel open={syncOpen} onClose={() => setSyncOpen(false)} />
        </Page>
    );
}

function ContactsTableBody({
    embedded,
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
    embedded?: boolean;
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
        categories?: { id: string; title: string; color: string }[];
        campaign_lead?: ContactCampaignProgress | null;
        created_at: Date;
    }[];
    selected: string[];
    onToggle: (id: string, on: boolean) => void;
    isSelectedAll: boolean;
    onToggleAll: () => void;
    onRowClick: (id: string, tab?: ContactSlideTab) => void;
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
                        <Th className="max-w-0 w-full md:max-w-none md:w-auto">Name</Th>
                        <Th className="hidden md:table-cell">Company</Th>
                        <Th className="hidden lg:table-cell">Phone</Th>
                        <Th className="w-auto md:w-32">{embedded ? "Progress" : "Status"}</Th>
                        {embedded ? (
                            <Th className="w-28 hidden md:table-cell">Current step</Th>
                        ) : (
                            <Th className="w-24 text-right hidden md:table-cell">Campaigns</Th>
                        )}
                        <Th className="w-24 text-right hidden md:table-cell">{embedded ? "Last activity" : "Added"}</Th>
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
                        // In the campaign Leads view, terminal leads (replied /
                        // bounced / unsubscribed) are "already processed" — render
                        // them muted so the eye lands on what's still in flight.
                        const lead = c.campaign_lead;
                        const processed =
                            embedded &&
                            !!lead &&
                            (lead.status === "replied" ||
                                lead.status === "bounced" ||
                                lead.status === "unsubscribed");
                        const isActiveLead = embedded && lead?.status === "active";
                        return (
                            <tr
                                key={c.id}
                                onClick={() => onRowClick(c.id)}
                                className={`group h-11 transition-colors cursor-pointer border-b border-slate-200/60 ${
                                    isSel
                                        ? "bg-sky-50/60"
                                        : isActiveLead
                                            ? "bg-sky-50/40 hover:bg-sky-50/70"
                                            : processed
                                                ? "bg-slate-50/40 hover:bg-slate-50/80"
                                                : "hover:bg-slate-50/80"
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
                                <td className="px-3 max-w-0 w-full md:max-w-none md:w-auto">
                                    <div className="flex items-center gap-2.5 min-w-0">
                                        <div className="w-6 h-6 rounded-full bg-slate-100 flex items-center justify-center shrink-0">
                                            <span className="text-[9.5px] font-semibold text-slate-600">
                                                {(c.first_name || c.email)?.slice(0, 2).toUpperCase()}
                                            </span>
                                        </div>
                                        <div className="min-w-0">
                                            <div className={`text-[12.5px] font-medium truncate leading-tight flex items-center gap-1.5 ${processed ? "text-slate-400" : "text-slate-900"}`}>
                                                <span className="truncate">{name}</span>
                                                {c.categories && c.categories.length > 0 && (
                                                    <span className="inline-flex items-center gap-0.5 shrink-0">
                                                        {c.categories.slice(0, 2).map((cat) => (
                                                            <CategoryChip key={cat.id} category={cat} compact />
                                                        ))}
                                                        {c.categories.length > 2 && (
                                                            <span
                                                                className="inline-flex items-center h-4 px-1 rounded text-[10px] font-medium bg-slate-100 text-slate-500"
                                                                title={c.categories.slice(2).map((x) => x.title).join(", ")}
                                                            >
                                                                +{c.categories.length - 2}
                                                            </span>
                                                        )}
                                                    </span>
                                                )}
                                            </div>
                                            <div className="text-[10.5px] text-slate-400 truncate font-mono leading-tight flex items-center gap-1">
                                                <MailIcon className="w-2.5 h-2.5 shrink-0" />
                                                <span className="truncate">{c.email}</span>
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
                                    {embedded ? (
                                        <LeadStatusPill lead={lead} />
                                    ) : (
                                        <StatusPill subscribed={c.subscribed} />
                                    )}
                                </td>
                                {embedded ? (
                                    <td className="px-3 hidden md:table-cell">
                                        {lead?.current_step ? (
                                            <span
                                                title={lead.current_step}
                                                className={`inline-flex items-center h-5 px-1.5 rounded text-[11px] font-medium max-w-[108px] ${
                                                    processed
                                                        ? "bg-slate-100 text-slate-400"
                                                        : "bg-sky-100 text-sky-700"
                                                }`}
                                            >
                                                <span className="truncate">{lead.current_step}</span>
                                            </span>
                                        ) : (
                                            <span className="text-[11px] text-slate-300">Not started</span>
                                        )}
                                    </td>
                                ) : (
                                    <td className="px-3 text-right font-mono text-[12px] text-slate-600 tabular-nums hidden md:table-cell">
                                        {c.campaigns?.length ?? 0}
                                    </td>
                                )}
                                <td className="px-3 text-right font-mono text-[11px] text-slate-500 tabular-nums hidden md:table-cell">
                                    {embedded
                                        ? lead?.last_activity_at
                                            ? new Date(lead.last_activity_at).toLocaleDateString("en-US", {
                                                  month: "short",
                                                  day: "numeric",
                                              })
                                            : "—"
                                        : c.created_at
                                            ? new Date(c.created_at).toLocaleDateString("en-US", {
                                                  month: "short",
                                                  day: "numeric",
                                              })
                                            : "—"}
                                </td>
                                <td className="px-3" onClick={(e) => e.stopPropagation()}>
                                    {/* Touch-safe: always visible on mobile, hover-reveal on desktop. */}
                                    <div className="flex items-center gap-0.5 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition-opacity">
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
                                            aria-label="Contact details"
                                            onClick={() => onRowClick(c.id, "details")}
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
                <span className="hidden sm:inline">subscribed</span>
            </span>
        );
    }
    return (
        <span className="inline-flex items-center gap-1 text-[10.5px] font-medium text-slate-500 uppercase tracking-[0.08em]">
            <span className="size-1.5 rounded-full bg-slate-300" />
            <span className="hidden sm:inline">unsubscribed</span>
        </span>
    );
}

// Per-lead processing state inside a campaign (campaign Leads view only).
// `active` renders the animated dot-grid loader (the same "processing" motif
// used across the app); every other state is a distinct lucide icon.
const LEAD_META: Record<
    LeadStatus,
    { label: string; dot: string; text: string; Icon: typeof ClockIcon }
> = {
    pending: { label: "Queued", dot: "bg-slate-300", text: "text-slate-500", Icon: ClockIcon },
    active: { label: "Processing", dot: "bg-sky-500", text: "text-sky-700", Icon: ClockIcon },
    completed: { label: "Done", dot: "bg-indigo-500", text: "text-indigo-700", Icon: CheckIcon },
    replied: { label: "Replied", dot: "bg-emerald-500", text: "text-emerald-700", Icon: CornerUpLeftIcon },
    bounced: { label: "Bounced", dot: "bg-rose-500", text: "text-rose-600", Icon: AlertTriangleIcon },
    unsubscribed: { label: "Unsubscribed", dot: "bg-slate-300", text: "text-slate-400", Icon: BanIcon },
};

function LeadStatusPill({ lead }: { lead?: ContactCampaignProgress | null }) {
    const status: LeadStatus = lead?.status ?? "pending";
    const meta = LEAD_META[status];
    const Icon = meta.Icon;
    return (
        <span
            className={`inline-flex items-center gap-1.5 text-[10.5px] font-medium uppercase tracking-[0.08em] ${meta.text}`}
        >
            {status === "active" ? (
                <span className="campaign-grid text-sky-600 shrink-0" aria-hidden />
            ) : (
                <Icon className="w-3 h-3 shrink-0" />
            )}
            <span className="hidden sm:inline">{meta.label}</span>
        </span>
    );
}

// Compact campaign-state strip above the Leads list: a segmented bar + per-state
// counts derived from the loaded leads, so you can see at a glance how the
// campaign is processing its leads.
function LeadProgressStrip({
    contacts,
    total,
    hasMore,
}: {
    contacts: { campaign_lead?: ContactCampaignProgress | null }[];
    total: number;
    hasMore: boolean;
}) {
    const counts = React.useMemo(() => {
        const c: Record<LeadStatus, number> = {
            pending: 0,
            active: 0,
            completed: 0,
            replied: 0,
            bounced: 0,
            unsubscribed: 0,
        };
        for (const ct of contacts) c[ct.campaign_lead?.status ?? "pending"]++;
        return c;
    }, [contacts]);

    const loaded = contacts.length;
    if (loaded === 0) return null;

    const segs: { key: LeadStatus; color: string }[] = [
        { key: "active", color: "bg-sky-500" },
        { key: "completed", color: "bg-indigo-500" },
        { key: "replied", color: "bg-emerald-500" },
        { key: "pending", color: "bg-slate-300" },
        { key: "bounced", color: "bg-rose-400" },
        { key: "unsubscribed", color: "bg-slate-200" },
    ];

    return (
        <div className="px-5 py-2.5 border-b border-slate-200/60 flex items-center gap-x-4 gap-y-2 flex-wrap">
            <div className="flex-1 min-w-[160px] max-w-[360px]">
                <div className="flex h-1.5 w-full overflow-hidden rounded-full bg-slate-100">
                    {segs.map((s) =>
                        counts[s.key] ? (
                            <div
                                key={s.key}
                                className={`${s.color} transition-[width] duration-500 ease-out`}
                                style={{ width: `${(counts[s.key] / loaded) * 100}%` }}
                            />
                        ) : null,
                    )}
                </div>
            </div>
            <div className="flex items-center gap-3 text-[11px] flex-wrap">
                <StripChip dot="bg-sky-500" label="Processing" n={counts.active} loader={counts.active > 0} />
                <StripChip dot="bg-indigo-500" label="Done" n={counts.completed} />
                <StripChip dot="bg-emerald-500" label="Replied" n={counts.replied} />
                <StripChip dot="bg-slate-300" label="Queued" n={counts.pending} />
                <StripChip dot="bg-rose-400" label="Bounced" n={counts.bounced} />
                <StripChip dot="bg-slate-300" label="Unsub" n={counts.unsubscribed} />
            </div>
            <div className="ml-auto flex items-center gap-2 text-[10.5px] text-slate-400 tabular-nums">
                {counts.active > 0 && (
                    <span className="inline-flex items-center gap-1 text-emerald-600 font-medium">
                        <span className="relative flex size-1.5">
                            <span className="absolute inline-flex h-full w-full rounded-full bg-emerald-500 opacity-60 animate-ping" />
                            <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
                        </span>
                        Live
                    </span>
                )}
                <span>
                    {hasMore ? `${loaded} of ${total} loaded` : `${total} lead${total === 1 ? "" : "s"}`}
                </span>
            </div>
        </div>
    );
}

function StripChip({
    dot,
    label,
    n,
    loader = false,
}: {
    dot: string;
    label: string;
    n: number;
    loader?: boolean;
}) {
    return (
        <span className="inline-flex items-center gap-1.5">
            {loader ? (
                <span className="campaign-grid text-sky-600" aria-hidden />
            ) : (
                <span className={`size-1.5 rounded-full ${dot}`} />
            )}
            <span className="text-slate-500">{label}</span>
            <span className="font-mono tabular-nums text-slate-900">{n}</span>
        </span>
    );
}

function SelectionBar({
    count,
    deleting,
    pushTargets,
    pushing,
    onPush,
    onBulkEdit,
    onResearch,
    researching,
    onDelete,
    onClear,
}: {
    count: number;
    deleting: boolean;
    pushTargets: IntegrationConnection[];
    pushing: boolean;
    onPush: (connectionId: string, providerLabel: string) => void;
    onBulkEdit: () => void;
    onResearch: () => void;
    researching: boolean;
    onDelete: () => void;
    onClear: () => void;
}) {
    if (count === 0) return null;
    return (
        <div className="absolute bottom-3 left-1/2 -translate-x-1/2 z-10 flex items-center max-w-[calc(100vw-16px)] flex-wrap justify-center md:max-w-none md:flex-nowrap gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5">
            <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                <CheckIcon className="w-3 h-3" />
                <span>{count} selected</span>
            </div>
            {pushTargets.length > 0 && (
                <PopoverMenu side="top" align="center">
                    <PopoverMenuTrigger asChild>
                        <button
                            type="button"
                            disabled={pushing}
                            className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                        >
                            {pushing ? (
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                            ) : (
                                <CableIcon className="w-3 h-3" />
                            )}
                            <span className="hidden sm:inline">Push to CRM</span>
                        </button>
                    </PopoverMenuTrigger>
                    <PopoverMenuContent>
                        <PopoverMenuLabel>Push {count} to</PopoverMenuLabel>
                        {pushTargets.map((t) => {
                            const label = PROVIDER_LABELS[t.provider];
                            const custom = t.label && t.label.toLowerCase() !== t.provider ? ` · ${t.label}` : "";
                            return (
                                <PopoverMenuItem key={t.id} onSelect={() => onPush(t.id, label)}>
                                    {label}
                                    {custom}
                                </PopoverMenuItem>
                            );
                        })}
                    </PopoverMenuContent>
                </PopoverMenu>
            )}
            <button
                type="button"
                onClick={onBulkEdit}
                className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 font-medium transition-colors"
            >
                Edit
            </button>
            <button
                type="button"
                onClick={onResearch}
                disabled={researching}
                className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-sky-700 hover:bg-sky-50 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
            >
                {researching ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <SparklesIcon className="w-3 h-3" />}
                <span className="hidden sm:inline">Research</span>
            </button>
            <button
                type="button"
                onClick={onDelete}
                disabled={deleting}
                className="h-7 px-2.5 rounded text-[12px] text-red-600 hover:text-white hover:bg-red-600 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
            >
                {deleting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <TrashIcon className="w-3 h-3" />}
                <span className="hidden sm:inline">Delete</span>
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
