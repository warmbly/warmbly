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
import { useNavigate, useSearchParams } from "react-router-dom";
import {
    AlertTriangleIcon,
    BanIcon,
    Building2Icon,
    CableIcon,
    CheckIcon,
    ClockIcon,
    CornerUpLeftIcon,
    DownloadIcon,
    InfoIcon,
    LayersIcon,
    Loader2Icon,
    MailIcon,
    MailOpenIcon,
    MoreHorizontalIcon,
    MousePointerClickIcon,
    PhoneIcon,
    PlusIcon,
    RefreshCcwIcon,
    Settings2Icon,
    ShieldCheckIcon,
    SheetIcon,
    SparklesIcon,
    TrashIcon,
    UploadIcon,
    UserMinusIcon,
    UserPlusIcon,
    UsersIcon,
    XIcon,
} from "lucide-react";

import { useConfirm } from "@/hooks/context/confirm";
import { useWriteGuard } from "@/hooks/usePermission";
import useSearchContacts from "@/lib/api/hooks/app/contacts/useSearchContacts";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import useDeleteContacts from "@/lib/api/hooks/app/contacts/useDeleteContacts";
import { useRequestContactVerification } from "@/lib/api/hooks/app/contacts/useContactVerification";
import VerificationBadge from "./VerificationBadge";
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
import clippedTitle from "@/lib/helper/clippedTitle";
import FilterBar from "./filters/FilterBar";
import { hasNarrowingFilters, isCompleteCustomFilter, scopeSearch } from "./filters/helpers";
import ContactEdit from "./ContactEdit";
import type { ContactSlideTab } from "./contact-edit/tabs";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { ContactCampaignProgress, LeadEngagement, LeadStatus, VerificationSource, VerificationStatus } from "@/lib/api/models/app/contacts/Contact";
import type { CampaignLeadCounts } from "@/lib/api/models/app/contacts/SearchContactsResult";
import ContactsEditBulk from "./ContactsEditBulk";
import { selectionOf } from "@/lib/api/models/app/contacts/ContactSelection";
import type ContactSelection from "@/lib/api/models/app/contacts/ContactSelection";
import * as rowSelection from "./selection";
import type { RowSelection } from "./selection";
import { NewContactDialog } from "./NewContactDialog";
import ExportDialog from "./ExportDialog";
import ImportWizard from "./ImportWizard";
import AddFromContactsDialog from "./AddFromContactsDialog";
import AddToSegmentMenu from "@/components/app/segments/AddToSegmentMenu";
import SegmentEditor from "@/components/app/segments/SegmentEditor";
import { filtersToSegment } from "@/components/app/segments/filtersToSegment";
import type { SegmentCondition } from "@/lib/api/models/app/segments/Segment";
import CampaignSegmentsDialog from "@/components/app/segments/CampaignSegmentsDialog";
import LinkedSegmentsStrip from "@/components/app/segments/LinkedSegmentsStrip";
import { linksEmptyReason } from "@/components/app/segments/linkedSegments";
import type { CampaignSegmentLink } from "@/lib/api/models/app/segments/Segment";
import { useAddSegmentToCampaign, useCampaignSegments, useSetSegmentMembers } from "@/lib/api/hooks/app/segments";
import type { ExportScopeContext } from "./ExportDialog";
import useUpdateContactsBulk from "@/lib/api/hooks/app/contacts/useUpdateContactsBulk";
import useAiMetered from "@/hooks/useAiMetered";
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

// Mirrors maxIntegrationPushSize on the backend: one synchronous push is a
// live call per contact against the CRM's API.
const MAX_CRM_PUSH = 500;

export default function ContactsTable({
    current_campaign,
    segment,
}: {
    current_campaign?: MiniCampaign;
    // Scope the list to one segment's members (the segment detail page).
    segment?: { id: string; name: string; color?: string };
}) {
    const confirm = useConfirm();
    const segmentMembers = useSetSegmentMembers();
    // Enrolling a segment writes campaign leads, so it takes the campaign permission.
    const campaignWrite = useWriteGuard("MANAGE_CAMPAIGNS");
    // Ticked rows, or "everything the search matches" minus what was unticked
    // after; see ./selection for the two shapes and what each one means.
    const [rowSel, setRowSel] = React.useState<RowSelection>(rowSelection.emptySelection);
    const [del, setDelete] = React.useState<boolean>(false);
    const [edit, setEdit] = React.useState<string>("");
    // Which tab the drawer opens on. Row click → default (overview); the
    // right-side 3-dots → "details", mirroring the mailbox 3-dots → settings.
    const [editTab, setEditTab] = React.useState<ContactSlideTab | undefined>(undefined);
    const openContact = React.useCallback((id: string, tab?: ContactSlideTab) => {
        setEditTab(tab);
        setEdit(id);
    }, []);
    const [bulkEdit, setBulkEdit] = React.useState<boolean>(false);
    const [newOpen, setNewOpen] = React.useState<boolean>(false);
    const [exportOpen, setExportOpen] = React.useState<boolean>(false);
    const [importOpen, setImportOpen] = React.useState<boolean>(false);
    const [syncOpen, setSyncOpen] = React.useState<boolean>(false);
    const [fromContactsOpen, setFromContactsOpen] = React.useState<boolean>(false);
    const [fromSegmentOpen, setFromSegmentOpen] = React.useState<boolean>(false);
    // A filter saved as a segment: the panel's draft becomes the editor's preset.
    const [segmentPreset, setSegmentPreset] = React.useState<{ conditions: SegmentCondition[] } | null>(null);
    const navigate = useNavigate();
    // ?category=<id> pre-filters the list (the Categories tab links here).
    const [params] = useSearchParams();

    const [searchProps, setSearchProps] = React.useState<SearchContacts>(() => {
        const category = params.get("category");
        return {
            ...scopeSearch({ campaignId: current_campaign?.id, segmentId: segment?.id }),
            category_ids: category && !segment && !current_campaign ? [category] : undefined,
        };
    });

    function saveAsSegment(draft: SearchContacts) {
        const { conditions, dropped } = filtersToSegment(draft, current_campaign?.id);
        if (dropped.length > 0) toast(`Not carried over: ${dropped.join(", ")}. Add a condition for it in the editor.`);
        setSegmentPreset({ conditions });
    }
    // Half-filled custom-field pills stay in the bar but never reach the server.
    const searchOptions = React.useMemo(
        () => ({ ...searchProps, custom_field_filters: searchProps.custom_field_filters.filter(isCompleteCustomFilter) }),
        [searchProps],
    );
    const contactsData = useSearchContacts({ options: searchOptions });

    // The stat strip's subscription facet is the same filter the bar exposes,
    // so it goes to the server: the total, the pages and "select all matching"
    // then all agree with what the strip says.
    const subFilter: SubFilter =
        searchProps.subscribed === undefined ? "all" : searchProps.subscribed ? "subscribed" : "unsubscribed";
    const setSubFilter = (v: SubFilter) =>
        setSearchProps((s) => ({ ...s, subscribed: v === "all" ? undefined : v === "subscribed" }));

    const clearSelection = React.useCallback(() => setRowSel(rowSelection.emptySelection), []);
    const isRowSelected = React.useCallback((id: string) => rowSelection.isRowSelected(rowSel, id), [rowSel]);
    // A different result set makes a select-all stale and a tick list
    // meaningless, so the selection resets with the filters. Sort is excluded:
    // it reorders the same rows.
    const selectionScope = React.useMemo(() => {
        const { sort_by: _sortBy, reverse: _reverse, ...rest } = searchOptions;
        return JSON.stringify(rest);
    }, [searchOptions]);
    React.useEffect(() => {
        clearSelection();
    }, [selectionScope, clearSelection]);

    // Inside a segment, "remove" pins the contact out as a manual exclude so
    // it stays out even while the conditions still match it.
    async function excludeFromSegment() {
        if (!segment || selectionCount === 0 || segmentMembers.isPending) return;
        try {
            const removed = await segmentMembers.mutateAsync({ id: segment.id, selection, mode: "exclude" });
            toast.success(`Removed ${removed.toLocaleString()} contact${removed === 1 ? "" : "s"} from ${segment.name}`);
            clearSelection();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }
    const contactsBulkDelete = useDeleteContacts();
    const bulkUpdate = useUpdateContactsBulk();
    // Linked segments (live audience) for the strip and badge on a Leads tab.
    const campaignSegments = useCampaignSegments(current_campaign?.id, !!current_campaign);
    const linkedSegments = React.useMemo(() => campaignSegments.data ?? [], [campaignSegments.data]);
    // Members removed from the campaign by hand are held out of automatic
    // enrolment; the one-shot enrol clears those records and adds them back.
    const reenrol = useAddSegmentToCampaign();
    const reenrolling = reenrol.isPending ? (reenrol.variables?.id ?? null) : null;
    // One link from the strip, or every link with held-out members from the
    // empty state; the one-shot enrol clears the removals segment by segment.
    function reenrolSegments(links: CampaignSegmentLink[]) {
        if (!current_campaign || reenrol.isPending || links.length === 0) return;
        const held = links.reduce((n, l) => n + l.held_out_count, 0);
        const names = links.map((l) => l.name).join(", ");
        confirm?.show(
            `Add the ${held.toLocaleString()} held-out member${held === 1 ? "" : "s"} of ${names} back to this campaign? Every current member of ${links.length === 1 ? "the segment" : "these segments"} becomes a lead again, including the ones removed by hand.`,
            async () => {
                let added = 0;
                try {
                    for (const l of links) {
                        const res = await reenrol.mutateAsync({ id: l.segment_id, campaignId: current_campaign.id });
                        added += res.added;
                    }
                    toast.success(`Added ${added.toLocaleString()} lead${added === 1 ? "" : "s"} back`);
                } catch (err) {
                    toast.error(buildError(err as AppError));
                }
            },
        );
    }
    // A linked-segment chip scopes the list to that segment's leads; clicking
    // the active one shows every lead again.
    const activeSegmentId = searchProps.segment_ids?.length === 1 ? searchProps.segment_ids[0] : undefined;
    const toggleSegmentScope = (id: string) =>
        setSearchProps((s) => ({ ...s, segment_ids: activeSegmentId === id ? undefined : [id] }));
    // The list's own scope with nothing narrowing it: what "clear filters"
    // returns to, and what "everyone" means in the export dialog here.
    const baseFilters = React.useMemo<SearchContacts>(
        () => ({
            ...scopeSearch({ campaignId: current_campaign?.id, segmentId: segment?.id }),
            sort_by: searchProps.sort_by,
            reverse: searchProps.reverse,
        }),
        [current_campaign, segment, searchProps.sort_by, searchProps.reverse],
    );
    const exportScope = React.useMemo<ExportScopeContext | undefined>(() => {
        if (current_campaign) return { kind: "campaign", name: current_campaign.name, baseFilters };
        if (segment) return { kind: "segment", name: segment.name, baseFilters };
        return undefined;
    }, [current_campaign, segment, baseFilters]);
    const [filterResetToken, setFilterResetToken] = React.useState(0);

    // In a campaign, "remove" detaches the leads; the contacts themselves stay.
    async function removeFromCampaign(target: ContactSelection, count: number) {
        if (!current_campaign || count === 0 || bulkUpdate.isPending) return;
        try {
            const updated = await bulkUpdate.mutateAsync({
                ...target,
                add_campaigns: [],
                remove_campaigns: [current_campaign.id],
                fields: [],
            });
            const n = updated.length || count;
            toast.success(`Removed ${n.toLocaleString()} lead${n === 1 ? "" : "s"} from ${current_campaign.name}`);
            clearSelection();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

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
        if (selectionCount === 0 || pushContacts.isPending) return;
        // The push is a live call per contact against the CRM, so the server
        // caps it; say so here instead of letting the request fail.
        if (selectionCount > MAX_CRM_PUSH) {
            toast.error(`Push to CRM takes up to ${MAX_CRM_PUSH.toLocaleString()} contacts at a time. Narrow the selection and try again.`);
            return;
        }
        const t = toast.loading(`Pushing ${selectionCount.toLocaleString()} to ${providerLabel}…`);
        try {
            const res = await pushContacts.mutateAsync({ connectionId, ...selection });
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
    const rows = React.useMemo(() => contacts ?? [], [contacts]);

    // What every bulk action applies to, and how many contacts that is. In
    // select-all mode the server resolves the filter, so the count here is the
    // search total minus whatever was unticked after.
    const loadedIDs = React.useMemo(() => rows.map((c) => c.id), [rows]);
    const selection = React.useMemo<ContactSelection>(
        () => rowSelection.toRequest(rowSel, searchOptions),
        [rowSel, searchOptions],
    );
    const selectionCount = rowSelection.selectionCount(rowSel, total);
    // Ticking every loaded row still leaves the rest of the match set behind;
    // that gap is what the banner offers to close.
    const loadedAllSelected = rowSelection.allLoadedSelected(rowSel, loadedIDs);
    const canSelectAllMatching = rowSelection.canSelectAllMatching(rowSel, loadedIDs, total);

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

    const toggleAll = () => setRowSel((s) => rowSelection.toggleLoaded(s, loadedIDs));
    const selectAllMatching = () => setRowSel(rowSelection.selectAllMatching());

    async function bulkDelete(target: ContactSelection, count: number) {
        if (count === 0) return;
        try {
            confirm?.setLoading(true);
            try {
                setDelete(true);
                await toast.promise(contactsBulkDelete.mutateAsync(target), {
                    loading: `Deleting ${count.toLocaleString()} ${count === 1 ? "contact" : "contacts"}…`,
                    success: count === 1 ? "Contact deleted" : "Contacts deleted",
                    error: (err: AppError) => buildError(err),
                });
                clearSelection();
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
    const metered = useAiMetered();
    function bulkResearch() {
        if (selectionCount === 0) return;
        confirm?.show(
            `Research ${selectionCount.toLocaleString()} ${selectionCount === 1 ? "contact" : "contacts"}? ${
                metered
                    ? `This uses up to ${(selectionCount * 2).toLocaleString()} AI credits and runs`
                    : "This runs"
            } in the background.`,
            async () => {
                const res = await batchResearch.mutateAsync({ selection, objective: "" });
                toast.success(`Queued research for ${res.queued.toLocaleString()} contacts`);
                clearSelection();
            },
        );
    }

    // Bulk verification actions. A re-check is queued and each row's mark
    // updates live as its verdict lands; marking deliverable is immediate.
    const verification = useRequestContactVerification();
    function bulkVerify() {
        if (selectionCount === 0) return;
        confirm?.show(
            `Re-verify ${selectionCount.toLocaleString()} ${selectionCount === 1 ? "address" : "addresses"}? Verdicts land in the background${
                selectionCount > 50 ? " over the next few minutes" : ""
            }.`,
            async () => {
                const res = await verification.mutateAsync({ ...selection, action: "verify" });
                toast.success(`Re-checking ${res.affected.toLocaleString()} ${res.affected === 1 ? "address" : "addresses"}`);
                clearSelection();
            },
        );
    }
    function bulkMarkDeliverable() {
        if (selectionCount === 0) return;
        confirm?.show(
            `Mark ${selectionCount.toLocaleString()} ${selectionCount === 1 ? "address" : "addresses"} deliverable? Campaigns will send to them even if verification refused them. Use this for a list you verified elsewhere.`,
            async () => {
                const res = await verification.mutateAsync({ ...selection, action: "mark_deliverable" });
                toast.success(`${res.affected.toLocaleString()} marked deliverable`);
                clearSelection();
            },
        );
    }

    const embedded = !!current_campaign;
    // Leads-view scope chips write straight into the search request, so the
    // rows, the total and pagination all come from the server for that scope.
    // Anything that narrows the list beyond its scope (the campaign or the
    // segment): an empty result then means "nothing matches", not "no leads".
    const narrowed = hasNarrowingFilters(searchProps, baseFilters);
    const setLeadStatus = (v: LeadStatus | undefined) =>
        setSearchProps((s) => ({ ...s, lead_status: s.lead_status === v ? undefined : v }));
    const setEngagement = (v: LeadEngagement | undefined) =>
        setSearchProps((s) => ({ ...s, engagement: s.engagement === v ? undefined : v }));
    const clearLeadFilters = () => {
        setSearchProps(baseFilters);
        setFilterResetToken((n) => n + 1);
    };
    // Why a campaign with linked segments still has no leads, in one line;
    // unknown while the links are still loading or failed to load.
    const linkedEmptyReason =
        current_campaign && campaignSegments.isSuccess && linkedSegments.length > 0
            ? linksEmptyReason(linkedSegments)
            : null;
    const linksPending = !!current_campaign && campaignSegments.isPending;
    const heldOutLinks = linkedSegments.filter((l) => l.held_out_count > 0);
    const heldOut = heldOutLinks.reduce((n, l) => n + l.held_out_count, 0);
    const canReenrol = heldOut > 0 && campaignWrite.allowed;
    const tableNode = (
        <ContactsTableBody
            embedded={embedded}
            isLoading={contactsData.isPending}
            isError={contactsData.isError}
            errorMessage={(contactsData.error as Error | undefined)?.message ?? "Request failed."}
            onRetry={() => contactsData.refetch()}
            isRefetching={contactsData.isFetching && !contactsData.isPending}
            contacts={rows}
            isRowSelected={isRowSelected}
            onToggle={(id, on) => setRowSel((s) => rowSelection.toggleRow(s, id, on))}
            isSelectedAll={loadedAllSelected}
            onToggleAll={toggleAll}
            banner={
                <SelectAllBanner
                    noun={current_campaign ? "lead" : segment ? "member" : "contact"}
                    selectAll={rowSel.all}
                    count={selectionCount}
                    loadedCount={rows.length}
                    total={total}
                    canSelectAllMatching={canSelectAllMatching}
                    onSelectAllMatching={selectAllMatching}
                    onClear={clearSelection}
                />
            }
            onRowClick={openContact}
            onDelete={(id) =>
                confirm?.show(`Delete this contact?`, async () => {
                    await bulkDelete(selectionOf(id), 1);
                })
            }
            onRemoveFromCampaign={
                embedded
                    ? (id) =>
                          confirm?.show(
                              "Remove this lead from the campaign? The contact stays in your workspace.",
                              async () => removeFromCampaign(selectionOf(id), 1),
                          )
                    : undefined
            }
            emptyTitle={
                subFilter !== "all"
                    ? `No ${subFilter} contacts`
                    : narrowed
                        ? current_campaign
                            ? "No leads match"
                            : "No contacts match"
                        : linkedEmptyReason
                            ? "No leads from your linked segments yet"
                            : linksPending
                                ? "No leads loaded yet"
                                : current_campaign
                                    ? "No contacts in this campaign"
                                : segment
                                    ? "No contacts in this segment"
                                    : "No contacts yet"
            }
            emptyBody={
                subFilter !== "all"
                    ? "Switch to All to see the full list."
                    : narrowed
                        ? `Clear the filters to see every ${current_campaign ? "lead" : "contact"}.`
                        : linkedEmptyReason
                            ? linkedEmptyReason
                            : linksPending
                                ? "Checking the campaign's linked segments…"
                                : current_campaign
                                    ? campaignSegments.isError
                                        ? "Pick people from your contacts, import a file, or add one by hand. The linked segments could not be loaded."
                                        : "Pick people from your contacts, link a segment, import a file, or add one by hand."
                                : segment
                                    ? "Nothing matches it yet. Pin people in from your contacts, import a file, or add one by hand."
                                    : "Add or upload contacts to get started."
            }
            emptyCta={
                subFilter !== "all" ? (
                    <TopbarAction variant="ghost" onClick={() => setSubFilter("all")}>
                        Show all
                    </TopbarAction>
                ) : narrowed ? (
                    <TopbarAction variant="ghost" onClick={clearLeadFilters}>
                        {current_campaign ? "Show all leads" : "Clear filters"}
                    </TopbarAction>
                ) : linksPending ? null : current_campaign ? (
                    <div className="flex flex-wrap items-center justify-center gap-1.5">
                        {canReenrol && (
                            <TopbarAction
                                icon={<UserPlusIcon className="w-3 h-3" />}
                                onClick={() => reenrolSegments(heldOutLinks)}
                                disabled={reenrol.isPending}
                            >
                                Add {heldOut.toLocaleString()} back
                            </TopbarAction>
                        )}
                        <TopbarAction
                            variant={canReenrol ? "ghost" : "primary"}
                            icon={<UsersIcon className="w-3 h-3" />}
                            onClick={() => setFromContactsOpen(true)}
                        >
                            From contacts
                        </TopbarAction>
                        <TopbarAction
                            variant="ghost"
                            icon={<LayersIcon className="w-3 h-3" />}
                            onClick={() => campaignWrite.guard(() => setFromSegmentOpen(true))({})}
                        >
                            {linkedSegments.length > 0 || campaignSegments.isError ? "Manage segments" : "Link a segment"}
                        </TopbarAction>
                        <TopbarAction
                            variant="ghost"
                            icon={<UploadIcon className="w-3 h-3" />}
                            onClick={() => setImportOpen(true)}
                        >
                            Import file
                        </TopbarAction>
                    </div>
                ) : segment ? (
                    <div className="flex flex-wrap items-center justify-center gap-1.5">
                        <TopbarAction
                            icon={<UsersIcon className="w-3 h-3" />}
                            onClick={() => setFromContactsOpen(true)}
                        >
                            Add contacts
                        </TopbarAction>
                        <TopbarAction
                            variant="ghost"
                            icon={<UploadIcon className="w-3 h-3" />}
                            onClick={() => setImportOpen(true)}
                        >
                            Import file
                        </TopbarAction>
                        <TopbarAction
                            variant="ghost"
                            icon={<UserPlusIcon className="w-3 h-3" />}
                            onClick={() => setNewOpen(true)}
                        >
                            New contact
                        </TopbarAction>
                    </div>
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
            nextPageFailed={contactsData.isFetchNextPageError}
            loadedCount={contacts?.length ?? 0}
            totalCount={total}
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
                        icon={<LayersIcon className="w-3 h-3" />}
                        onClick={() => campaignWrite.guard(() => setFromSegmentOpen(true))({})}
                    >
                        Segments
                        {(campaignSegments.data?.length ?? 0) > 0 ? ` (${campaignSegments.data?.length})` : ""}
                    </TopbarAction>
                    <TopbarAction
                        variant="ghost"
                        icon={<UsersIcon className="w-3 h-3" />}
                        onClick={() => setFromContactsOpen(true)}
                    >
                        From contacts
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
                        variant="ghost"
                        icon={<DownloadIcon className="w-3 h-3" />}
                        onClick={() => setExportOpen(true)}
                    >
                        Export
                    </TopbarAction>
                    <TopbarAction
                        icon={<UserPlusIcon className="w-3 h-3" />}
                        onClick={() => setNewOpen(true)}
                    >
                        Add lead
                    </TopbarAction>
                </SectionBar>
                <LinkedSegmentsStrip
                    links={linkedSegments}
                    activeSegmentId={activeSegmentId}
                    onToggle={toggleSegmentScope}
                    onManage={() => campaignWrite.guard(() => setFromSegmentOpen(true))({})}
                    onReenrol={campaignWrite.allowed ? (l) => reenrolSegments([l]) : undefined}
                    reenrolling={reenrolling}
                />
                <FilterBar
                    filters={searchProps}
                    setFilters={setSearchProps}
                    activeCampaign={current_campaign}
                    resetToken={filterResetToken}
                    total={total}
                    loading={contactsData.isFetching}
                    onSaveAsSegment={saveAsSegment}
                />
                <LeadProgressStrip
                    contacts={contacts ?? []}
                    total={total}
                    hasMore={!!contactsData.hasNextPage}
                    serverCounts={contactsData.data?.pages[0]?.lead_counts}
                    leadStatus={searchProps.lead_status}
                    engagement={searchProps.engagement}
                    onLeadStatus={setLeadStatus}
                    onEngagement={setEngagement}
                />
                {tableNode}
                <SelectionBar
                    count={selectionCount}
                    deleting={del}
                    pushTargets={pushTargets}
                    pushing={pushContacts.isPending}
                    onPush={pushToCRM}
                    onBulkEdit={() => setBulkEdit(true)}
                    onResearch={bulkResearch}
                    researching={batchResearch.isPending}
                    onVerify={bulkVerify}
                    onMarkDeliverable={bulkMarkDeliverable}
                    verifying={verification.isPending}
                    onDelete={() =>
                        confirm?.show(
                            deletePrompt(selectionCount),
                            () => bulkDelete(selection, selectionCount),
                        )
                    }
                    onClear={clearSelection}
                    selection={selection}
                    segment={segment}
                    onExclude={excludeFromSegment}
                    excluding={segmentMembers.isPending}
                    campaign={current_campaign}
                    onRemoveFromCampaign={() =>
                        confirm?.show(
                            `Remove ${selectionCount.toLocaleString()} lead${selectionCount === 1 ? "" : "s"} from this campaign? The contacts stay in your workspace.`,
                            async () => removeFromCampaign(selection, selectionCount),
                        )
                    }
                    removing={bulkUpdate.isPending}
                />
                <ContactEdit
                    contacts={contacts ?? []}
                    active={edit}
                    setActive={setEdit}
                />
                <ContactsEditBulk
                    active={bulkEdit}
                    setActive={setBulkEdit}
                    selection={selection}
                    count={selectionCount}
                    onDone={clearSelection}
                    scope={current_campaign ? { kind: "campaign", name: current_campaign.name } : undefined}
                />
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
                <AddFromContactsDialog
                    open={fromContactsOpen}
                    onClose={() => setFromContactsOpen(false)}
                    campaign={current_campaign}
                />
                <CampaignSegmentsDialog
                    open={fromSegmentOpen}
                    onClose={() => setFromSegmentOpen(false)}
                    campaign={current_campaign}
                />
                <ExportDialog
                    open={exportOpen}
                    onClose={() => setExportOpen(false)}
                    filters={searchOptions}
                    selectedIds={rowSel.all ? [] : rowSel.ids}
                    totalKnown={total}
                    scopeContext={exportScope}
                />
            </>
        );
    }

    return (
        <Page>
            <PageTopbar
                eyebrow={segment ? "Members" : "Contacts"}
                subtitle={
                    contactsData.isPending
                        ? "Loading…"
                        : contactsData.isError
                            ? "Failed to load"
                            : segment
                              ? `${total.toLocaleString()} in ${segment.name}`
                              : `${total.toLocaleString()} total`
                }
            >
                {segment && (
                    <TopbarAction
                        variant="ghost"
                        icon={<UsersIcon className="w-3 h-3" />}
                        onClick={() => setFromContactsOpen(true)}
                    >
                        Add contacts
                    </TopbarAction>
                )}
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

            {!segment && <StatStrip cols={4}>
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
            </StatStrip>}

            <SectionBar
                label={segment ? "Segment members" : subFilter === "all" ? "All contacts" : `${subFilter[0].toUpperCase()}${subFilter.slice(1)}`}
                count={total}
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
            </SectionBar>

            <FilterBar
                filters={searchProps}
                setFilters={setSearchProps}
                hideSegments={!!segment}
                resetToken={filterResetToken}
                total={total}
                loading={contactsData.isFetching}
                onSaveAsSegment={saveAsSegment}
            />

            <PageBody>
                {tableNode}
            </PageBody>

            <SelectionBar
                count={selectionCount}
                deleting={del}
                pushTargets={pushTargets}
                pushing={pushContacts.isPending}
                onPush={pushToCRM}
                onBulkEdit={() => setBulkEdit(true)}
                onResearch={bulkResearch}
                researching={batchResearch.isPending}
                onVerify={bulkVerify}
                onMarkDeliverable={bulkMarkDeliverable}
                verifying={verification.isPending}
                onDelete={() =>
                    confirm?.show(
                        deletePrompt(selectionCount),
                        () => bulkDelete(selection, selectionCount),
                    )
                }
                onClear={clearSelection}
                selection={selection}
                segment={segment}
                onExclude={excludeFromSegment}
                excluding={segmentMembers.isPending}
            />

            <SegmentEditor
                open={segmentPreset !== null}
                onClose={() => setSegmentPreset(null)}
                preset={segmentPreset}
                onSaved={(saved) => navigate(`/app/contacts/segments/${saved.id}`)}
            />
            <ContactEdit contacts={contacts ?? []} active={edit} setActive={setEdit} initialTab={editTab} />
            <ContactsEditBulk
                active={bulkEdit}
                setActive={setBulkEdit}
                selection={selection}
                count={selectionCount}
                onDone={clearSelection}
                scope={segment ? { kind: "segment", name: segment.name } : undefined}
            />
            <NewContactDialog open={newOpen} onClose={() => setNewOpen(false)} segment={segment} />
            {segment && (
                <AddFromContactsDialog
                    open={fromContactsOpen}
                    onClose={() => setFromContactsOpen(false)}
                    target={{ kind: "segment", segment }}
                />
            )}
            <ExportDialog
                open={exportOpen}
                onClose={() => setExportOpen(false)}
                filters={searchOptions}
                selectedIds={rowSel.all ? [] : rowSel.ids}
                totalKnown={total}
                scopeContext={exportScope}
            />
            <ImportWizard
                open={importOpen}
                onClose={() => setImportOpen(false)}
                lockedSegment={segment}
            />
            <SyncSourcesPanel open={syncOpen} onClose={() => setSyncOpen(false)} segment={segment} />
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
    isRowSelected,
    onToggle,
    isSelectedAll,
    onToggleAll,
    banner,
    onRowClick,
    onDelete,
    onRemoveFromCampaign,
    emptyTitle,
    emptyBody,
    emptyCta,
    hasNextPage,
    isFetchingNextPage,
    onLoadMore,
    nextPageFailed,
    loadedCount,
    totalCount,
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
        verification_status?: VerificationStatus;
        verification_sub_status?: string;
        verification_source?: VerificationSource;
        verification_provider?: string;
        verification_checked_at?: string | null;
        verification_confidence?: number;
        created_at: Date;
    }[];
    isRowSelected: (id: string) => boolean;
    onToggle: (id: string, on: boolean) => void;
    isSelectedAll: boolean;
    onToggleAll: () => void;
    // Sits between the header and the rows: "select all N matching".
    banner: React.ReactNode;
    onRowClick: (id: string, tab?: ContactSlideTab) => void;
    onDelete: (id: string) => void;
    // In a campaign, the row's destructive action detaches the lead instead
    // of deleting the contact from the whole workspace.
    onRemoveFromCampaign?: (id: string) => void;
    emptyTitle: string;
    emptyBody: string;
    emptyCta: React.ReactNode;
    hasNextPage: boolean;
    isFetchingNextPage: boolean;
    onLoadMore: () => void;
    // Whether the failure was the next page rather than a refetch of the whole
    // list, which decides what "Try again" runs.
    nextPageFailed: boolean;
    // How far through the list we are, so "Load more" says how much is left.
    loadedCount: number;
    totalCount: number;
}) {
    // Name is the only auto-width column, so it takes every pixel the sized
    // columns leave — under table-fixed a second auto column would split that
    // slack with it and size the name like a phone number. Which breakpoint each
    // sized column appears at is then just "does Name still clear ~170px": Leads
    // carries five campaign columns Contacts does not, so company waits longer
    // for room there, and a phone number is no part of reading a campaign, so
    // Leads drops that column and the contact drawer keeps the address.
    const companyCol = embedded ? "w-40 hidden xl:table-cell" : "w-36 hidden lg:table-cell";
    const phoneCol = "w-36 hidden xl:table-cell";

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
    // A page that fails once rows are loaded is reported in the footer instead;
    // throwing away 500 loaded leads because page 13 failed is worse than the
    // failure. The count is of rows LOADED, not of rows left after the
    // client-side subscription filter, which can hide all of them.
    if (isError && loadedCount === 0) {
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
    // "Try again" runs the request that actually failed: the page that did not
    // arrive, or a refetch of the whole list when it was the refetch that broke.
    const retry = nextPageFailed ? onLoadMore : onRetry;
    const retrying = nextPageFailed ? isFetchingNextPage : isRefetching;
    const footer = isError ? (
        <div className="px-5 py-3 flex flex-col items-center gap-2 border-t border-slate-200/60">
            <p className="text-[11.5px] text-slate-500 text-center max-w-[52ch] leading-relaxed">
                <AlertTriangleIcon className="w-3 h-3 inline-block mr-1 -mt-px text-red-500" />
                {errorMessage}
            </p>
            <button
                type="button"
                onClick={retry}
                disabled={retrying}
                className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
            >
                {retrying ? (
                    <Loader2Icon className="w-3 h-3 animate-spin" />
                ) : (
                    <RefreshCcwIcon className="w-3 h-3" />
                )}
                Try again
            </button>
        </div>
    ) : hasNextPage ? (
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
                        {totalCount > loadedCount && (
                            <span className="text-slate-400">
                                · {loadedCount.toLocaleString()} of {totalCount.toLocaleString()}
                            </span>
                        )}
                    </>
                )}
            </button>
        </div>
    ) : null;

    // Rows are loaded but the sub-filter hides all of them: still say so, and
    // still surface a failed page rather than swallowing it.
    if (contacts.length === 0) {
        return (
            <>
                <EmptyBlock title={emptyTitle} body={emptyBody} cta={emptyCta} />
                {footer}
            </>
        );
    }
    return (
        <>
            {banner}
            {/* table-fixed, not auto: under auto layout one long company name
                sets its column's min-content and widens the table past the
                panel, which is what put a horizontal scrollbar under the list
                (issue #461). So every column carries a width and every cell
                clips; content that overflows a cell still extends the scroll
                container. */}
            <table className="w-full table-fixed text-left">
                <thead className="sticky top-0 bg-white z-[1]">
                    <tr className="border-b border-slate-200">
                        <th className="pl-5 pr-2 py-2 w-11">
                            <input
                                type="checkbox"
                                className="w-3.5 h-3.5 rounded accent-sky-600"
                                checked={isSelectedAll}
                                onChange={onToggleAll}
                            />
                        </th>
                        <Th>Name</Th>
                        <Th className={companyCol}>Company</Th>
                        {!embedded && <Th className={phoneCol}>Phone</Th>}
                        <Th className="w-12 sm:w-32">
                            {/* Below sm the pill is its icon alone, so the column
                                narrows to it and the label waits for the room —
                                but never leaves the accessibility tree. */}
                            <span className="sr-only">{embedded ? "Progress" : "Status"}</span>
                            <span aria-hidden className="hidden sm:inline">{embedded ? "Progress" : "Status"}</span>
                        </Th>
                        {embedded && (
                            <>
                                <Th className="w-24 hidden lg:table-cell">
                                    <span className="inline-flex items-center gap-1">
                                        Opened
                                        <span
                                            className="inline-flex cursor-help text-slate-300 hover:text-slate-500"
                                            title="Opens rely on the mail client loading images. Clients that block images show no open even when the email was read. A click by the person always counts as an open."
                                        >
                                            <InfoIcon className="w-3 h-3" aria-label="How opens are counted" />
                                        </span>
                                    </span>
                                </Th>
                                <Th className="w-24 hidden lg:table-cell">Clicked</Th>
                                <Th className="w-24 hidden lg:table-cell">Replied</Th>
                            </>
                        )}
                        {embedded ? (
                            <>
                                <Th className="w-32 hidden xl:table-cell">Current step</Th>
                                <Th className="w-36 hidden 2xl:table-cell">
                                    <span className="inline-flex items-center gap-1">
                                        Sender
                                        <span
                                            className="inline-flex cursor-help text-slate-300 hover:text-slate-500"
                                            title="The mailbox this lead's whole sequence sends from. It is picked when the first email goes out and every follow-up keeps it, so the contact always hears from one address."
                                        >
                                            <InfoIcon className="w-3 h-3" aria-label="How the sender is chosen" />
                                        </span>
                                    </span>
                                </Th>
                            </>
                        ) : (
                            <Th className="w-28 text-right hidden lg:table-cell">Campaigns</Th>
                        )}
                        <Th className={`text-right ${embedded ? "w-32 hidden 2xl:table-cell" : "w-24 hidden md:table-cell"}`}>
                            {embedded ? "Last activity" : "Added"}
                        </Th>
                        <th className="px-3 py-2 w-[76px]"></th>
                    </tr>
                </thead>
                <tbody>
                    {contacts.map((c) => {
                        const isSel = isRowSelected(c.id);
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
                                <td className="px-3 overflow-hidden">
                                    <div className="flex items-center gap-2.5 min-w-0">
                                        <div className="w-6 h-6 rounded-full bg-slate-100 flex items-center justify-center shrink-0">
                                            <span className="text-[9.5px] font-semibold text-slate-600">
                                                {(c.first_name || c.email)?.slice(0, 2).toUpperCase()}
                                            </span>
                                        </div>
                                        {/* flex-1, not shrink-to-fit: the chip cap below is a
                                            percentage, so this has to be the column's width and
                                            not the name's. */}
                                        <div className="flex-1 min-w-0">
                                            <div className={`text-[12.5px] font-medium truncate leading-tight flex items-center gap-1.5 ${processed ? "text-slate-400" : "text-slate-900"}`}>
                                                <span className="truncate" {...clippedTitle}>{name}</span>
                                                {/* Neither side of this line may eat the other: shrink-0 chips
                                                    take a fixed-width Name cell whole and leave no name, while
                                                    a freely shrinking group collapses to bare colour dots. Cap
                                                    the tags at 45% and let both ellipsize into their tooltip. */}
                                                {c.categories && c.categories.length > 0 && (
                                                    <span className="inline-flex items-center gap-0.5 min-w-0 max-w-[45%]">
                                                        {c.categories.slice(0, 2).map((cat) => (
                                                            <CategoryChip key={cat.id} category={cat} compact clamp />
                                                        ))}
                                                        {c.categories.length > 2 && (
                                                            <span
                                                                className="inline-flex items-center h-4 px-1 shrink-0 rounded text-[10px] font-medium bg-slate-100 text-slate-500"
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
                                                <span className="truncate" {...clippedTitle}>{c.email}</span>
                                                <VerificationBadge contact={c} />
                                            </div>
                                        </div>
                                    </div>
                                </td>
                                <td className={`px-3 overflow-hidden text-[12px] text-slate-600 ${companyCol}`}>
                                    {c.company ? (
                                        <div className="flex items-center gap-1.5 min-w-0">
                                            <Building2Icon className="w-3 h-3 shrink-0 text-slate-400" />
                                            <span className="truncate" {...clippedTitle}>
                                                {c.company}
                                            </span>
                                        </div>
                                    ) : (
                                        <span className="text-slate-300">—</span>
                                    )}
                                </td>
                                {!embedded && (
                                    <td className={`px-3 overflow-hidden text-[12px] text-slate-600 font-mono ${phoneCol}`}>
                                        {c.phone ? (
                                            <div className="flex items-center gap-1.5 min-w-0">
                                                <PhoneIcon className="w-3 h-3 shrink-0 text-slate-400" />
                                                <span className="truncate" {...clippedTitle}>
                                                    {c.phone}
                                                </span>
                                            </div>
                                        ) : (
                                            <span className="text-slate-300">—</span>
                                        )}
                                    </td>
                                )}
                                <td className="px-3 overflow-hidden">
                                    {embedded ? (
                                        <LeadStatusPill lead={lead} />
                                    ) : (
                                        <StatusPill subscribed={c.subscribed} />
                                    )}
                                </td>
                                {embedded && (
                                    <>
                                        <EngagementCell
                                            n={lead?.opened ?? 0}
                                            sent={(lead?.sent ?? 0) > 0}
                                            Icon={MailOpenIcon}
                                            label="opened"
                                            auto={(lead?.machine_opened ?? 0) > 0}
                                        />
                                        <EngagementCell
                                            n={lead?.clicked ?? 0}
                                            sent={(lead?.sent ?? 0) > 0}
                                            Icon={MousePointerClickIcon}
                                            label="clicked"
                                        />
                                        <EngagementCell
                                            n={lead?.replied ?? 0}
                                            sent={(lead?.sent ?? 0) > 0}
                                            Icon={CornerUpLeftIcon}
                                            label="replied"
                                        />
                                    </>
                                )}
                                {embedded ? (
                                    <>
                                    <td className="px-3 overflow-hidden hidden xl:table-cell">
                                        {lead?.current_step ? (
                                            <span
                                                title={lead.current_step}
                                                className={`inline-flex items-center h-5 px-1.5 rounded text-[11px] font-medium max-w-full ${
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
                                    <td className="px-3 overflow-hidden hidden 2xl:table-cell">
                                        {lead?.sender ? (
                                            <span
                                                title={`Every step of this lead's sequence sends from ${lead.sender}`}
                                                className="block truncate text-[11.5px] text-slate-600"
                                            >
                                                {lead.sender}
                                            </span>
                                        ) : (
                                            <span className="text-[11px] text-slate-300">Not assigned</span>
                                        )}
                                    </td>
                                    </>
                                ) : (
                                    <td className="px-3 text-right font-mono text-[12px] text-slate-600 tabular-nums hidden lg:table-cell">
                                        {c.campaigns?.length ?? 0}
                                    </td>
                                )}
                                <td className={`px-3 text-right font-mono text-[11px] text-slate-500 tabular-nums ${embedded ? "hidden 2xl:table-cell" : "hidden md:table-cell"}`}>
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
                                        {onRemoveFromCampaign ? (
                                            <button
                                                type="button"
                                                aria-label="Remove from campaign"
                                                onClick={() => onRemoveFromCampaign(c.id)}
                                                className="size-6 rounded text-slate-400 hover:text-amber-600 hover:bg-amber-50 flex items-center justify-center transition-colors"
                                            >
                                                <UserMinusIcon className="w-3 h-3" />
                                            </button>
                                        ) : (
                                            <button
                                                type="button"
                                                aria-label="Delete contact"
                                                onClick={() => onDelete(c.id)}
                                                className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 flex items-center justify-center transition-colors"
                                            >
                                                <TrashIcon className="w-3 h-3" />
                                            </button>
                                        )}
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
            {footer}
        </>
    );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <th
            className={`px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] truncate ${className ?? ""}`}
        >
            {children}
        </th>
    );
}

function StatusPill({ subscribed }: { subscribed: boolean }) {
    const label = subscribed ? "subscribed" : "unsubscribed";
    return (
        <span
            className={`inline-flex items-center gap-1 max-w-full text-[10.5px] font-medium uppercase tracking-[0.08em] ${
                subscribed ? "text-emerald-700" : "text-slate-500"
            }`}
        >
            <span
                className={`size-1.5 shrink-0 rounded-full ${subscribed ? "bg-emerald-500" : "bg-slate-300"}`}
            />
            {/* The dot carries the state on its own below sm, so the word stays
                for screen readers at every width and the visible copy is the
                one that comes and goes. */}
            <span className="sr-only">{label}</span>
            <span aria-hidden className="hidden sm:inline truncate" {...clippedTitle}>
                {label}
            </span>
        </span>
    );
}

// One engagement column of the Leads view. A count of steps engaged, a dash
// for a lead that was sent but never did, and blank for a lead never emailed.
// A machine-only open (Apple MPP prefetch) reads "auto" so it is not mistaken
// for a person.
function EngagementCell({
    n,
    sent,
    Icon,
    label,
    auto = false,
}: {
    n: number;
    sent: boolean;
    Icon: typeof MailOpenIcon;
    label: string;
    auto?: boolean;
}) {
    return (
        <td className="px-3 overflow-hidden hidden lg:table-cell">
            {n > 0 ? (
                <span
                    className="inline-flex items-center gap-1 text-[11px] font-medium text-emerald-700 tabular-nums"
                    title={`${label} ${n} ${n === 1 ? "email" : "emails"}`}
                >
                    <Icon className="w-3 h-3 shrink-0" />
                    {n}
                </span>
            ) : auto ? (
                <span
                    className="text-[10.5px] text-slate-400"
                    title="Opened by a mail client automatically, not by a person"
                >
                    auto
                </span>
            ) : sent ? (
                <span className="text-slate-300 text-[11px]" aria-label={`not ${label}`}>
                    —
                </span>
            ) : null}
        </td>
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
    failed: { label: "Failed", dot: "bg-rose-500", text: "text-rose-600", Icon: AlertTriangleIcon },
    unsubscribed: { label: "Unsubscribed", dot: "bg-slate-300", text: "text-slate-400", Icon: BanIcon },
    undeliverable: { label: "Undeliverable", dot: "bg-amber-500", text: "text-amber-600", Icon: AlertTriangleIcon },
};

function LeadStatusPill({ lead }: { lead?: ContactCampaignProgress | null }) {
    const status: LeadStatus = lead?.status ?? "pending";
    const meta = LEAD_META[status];
    const Icon = meta.Icon;
    // A failed lead carries the worker's reason; surface it on hover since the
    // pill itself only has room for the word.
    const title =
        status === "failed" && lead?.failure_reason
            ? `Could not send: ${lead.failure_reason}`
            : status === "undeliverable"
                ? "Address verification refused this recipient, so the campaign skips it"
                : undefined;
    return (
        <span
            className={`inline-flex items-center gap-1.5 max-w-full text-[10.5px] font-medium uppercase tracking-[0.08em] ${meta.text}`}
            title={title}
        >
            {status === "active" ? (
                <span className="campaign-grid text-sky-600 shrink-0" aria-hidden />
            ) : (
                <Icon className="w-3 h-3 shrink-0" />
            )}
            <span className="sr-only">{meta.label}</span>
            <span aria-hidden className="hidden sm:inline truncate" {...(title ? {} : clippedTitle)}>
                {meta.label}
            </span>
        </span>
    );
}

// Compact campaign-state strip above the Leads list: a segmented bar + per-state
// counts, so you can see at a glance how the campaign is processing its leads.
//
// The numbers come from the server's campaign-wide `lead_counts` whenever it is
// there. Counting the loaded rows instead made the strip lie on any campaign
// past one page: a campaign of 57 leads showed "Queued 50, Done 0" purely
// because those were the 50 rows on screen (issue #189). The loaded-row count
// stays as the fallback so the strip is never blank on first paint.
function LeadProgressStrip({
    contacts,
    total,
    hasMore,
    serverCounts,
    leadStatus,
    engagement,
    onLeadStatus,
    onEngagement,
}: {
    contacts: { campaign_lead?: ContactCampaignProgress | null }[];
    total: number;
    hasMore: boolean;
    serverCounts?: CampaignLeadCounts;
    leadStatus?: LeadStatus;
    engagement?: LeadEngagement;
    onLeadStatus: (s: LeadStatus) => void;
    onEngagement: (e: LeadEngagement) => void;
}) {
    const counts = React.useMemo(() => {
        if (serverCounts) {
            return {
                pending: serverCounts.queued,
                active: serverCounts.processing,
                completed: serverCounts.completed,
                replied: serverCounts.replied,
                bounced: serverCounts.bounced,
                failed: serverCounts.failed,
                unsubscribed: serverCounts.unsubscribed,
                undeliverable: serverCounts.undeliverable ?? 0,
            } satisfies Record<LeadStatus, number>;
        }
        const c: Record<LeadStatus, number> = {
            pending: 0,
            active: 0,
            completed: 0,
            replied: 0,
            bounced: 0,
            failed: 0,
            unsubscribed: 0,
            undeliverable: 0,
        };
        for (const ct of contacts) c[ct.campaign_lead?.status ?? "pending"]++;
        return c;
    }, [contacts, serverCounts]);

    const loaded = contacts.length;
    // Stay mounted while a chip filter is active, or an empty scope would
    // take the only control that clears it off the screen.
    if (loaded === 0 && !leadStatus && !engagement && !(serverCounts && serverCounts.total > 0)) return null;
    // The bar's segments are shares of whatever the counts cover.
    const barTotal = serverCounts ? Math.max(serverCounts.total, 1) : loaded;
    const status = (key: LeadStatus) => ({
        active: leadStatus === key,
        onClick: () => onLeadStatus(key),
    });
    const engaged = (key: LeadEngagement) => ({
        active: engagement === key,
        onClick: () => onEngagement(key),
    });
    // Engagement totals only exist server-side; the fallback row count would
    // lie past one page, so the chips carry no number until they arrive.
    const eng = serverCounts
        ? {
              opened: serverCounts.opened,
              notOpened: Math.max(serverCounts.contacted - serverCounts.opened, 0),
              clicked: serverCounts.clicked,
              notClicked: Math.max(serverCounts.contacted - serverCounts.clicked, 0),
              replied: serverCounts.replied_any,
              notReplied: Math.max(serverCounts.contacted - serverCounts.replied_any, 0),
          }
        : undefined;

    const segs: { key: LeadStatus; color: string }[] = [
        { key: "active", color: "bg-sky-500" },
        { key: "completed", color: "bg-indigo-500" },
        { key: "replied", color: "bg-emerald-500" },
        { key: "pending", color: "bg-slate-300" },
        { key: "bounced", color: "bg-rose-400" },
        { key: "failed", color: "bg-rose-500" },
        { key: "unsubscribed", color: "bg-slate-200" },
        { key: "undeliverable", color: "bg-amber-500" },
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
                                style={{ width: `${(counts[s.key] / barTotal) * 100}%` }}
                            />
                        ) : null,
                    )}
                </div>
            </div>
            <div className="flex items-center gap-2 text-[11px] flex-wrap">
                <StripChip dot="bg-sky-500" label="Processing" n={counts.active} loader={counts.active > 0} {...status("active")} />
                <StripChip dot="bg-indigo-500" label="Done" n={counts.completed} {...status("completed")} />
                <StripChip dot="bg-emerald-500" label="Replied" n={counts.replied} {...status("replied")} />
                <StripChip dot="bg-slate-300" label="Queued" n={counts.pending} {...status("pending")} />
                <StripChip dot="bg-rose-400" label="Bounced" n={counts.bounced} {...status("bounced")} />
                {(counts.failed > 0 || leadStatus === "failed") && (
                    <StripChip dot="bg-rose-500" label="Failed" n={counts.failed} {...status("failed")} />
                )}
                {(counts.undeliverable > 0 || leadStatus === "undeliverable") && (
                    <StripChip dot="bg-amber-500" label="Undeliverable" n={counts.undeliverable} {...status("undeliverable")} />
                )}
                <StripChip dot="bg-slate-300" label="Unsub" n={counts.unsubscribed} {...status("unsubscribed")} />
                <span className="h-4 w-px bg-slate-200 mx-0.5" aria-hidden />
                <StripChip Icon={MailOpenIcon} label="Opened" n={eng?.opened} {...engaged("opened")} />
                <StripChip Icon={MailOpenIcon} label="Not opened" n={eng?.notOpened} {...engaged("not_opened")} />
                <StripChip Icon={MousePointerClickIcon} label="Clicked" n={eng?.clicked} {...engaged("clicked")} />
                <StripChip Icon={MousePointerClickIcon} label="Not clicked" n={eng?.notClicked} {...engaged("not_clicked")} />
                <StripChip Icon={CornerUpLeftIcon} label="Replied" n={eng?.replied} {...engaged("replied")} />
                <StripChip Icon={CornerUpLeftIcon} label="Not replied" n={eng?.notReplied} {...engaged("not_replied")} />
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

// A scope chip: click toggles that scope as the server filter. Status chips
// carry a colour dot, engagement chips a lucide icon; `n` is omitted while the
// server total is not known yet.
function StripChip({
    dot,
    Icon,
    label,
    n,
    loader = false,
    active = false,
    onClick,
}: {
    dot?: string;
    Icon?: typeof MailOpenIcon;
    label: string;
    n?: number;
    loader?: boolean;
    active?: boolean;
    onClick: () => void;
}) {
    return (
        <button
            type="button"
            aria-pressed={active}
            onClick={onClick}
            className={`inline-flex items-center gap-1.5 h-6 px-1.5 -mx-0.5 rounded-md border transition-colors ${
                active
                    ? "border-sky-200 bg-sky-50 text-sky-700"
                    : "border-transparent hover:bg-slate-100 text-slate-500"
            }`}
        >
            {loader ? (
                <span className="campaign-grid text-sky-600" aria-hidden />
            ) : Icon ? (
                <Icon className={`w-3 h-3 ${active ? "text-sky-600" : "text-slate-400"}`} />
            ) : (
                <span className={`size-1.5 rounded-full ${dot}`} />
            )}
            <span className={active ? "text-sky-700" : "text-slate-500"}>{label}</span>
            {n !== undefined && (
                <span className={`font-mono tabular-nums ${active ? "text-sky-800" : "text-slate-900"}`}>{n}</span>
            )}
        </button>
    );
}

// deletePrompt words the confirm so a 12,000-row select-all does not read the
// same as three ticked rows.
function deletePrompt(count: number): string {
    if (count === 1) return "Delete this contact? It is removed from every campaign and segment it belongs to.";
    return `Delete ${count.toLocaleString()} contacts? They are removed from every campaign and segment they belong to. This cannot be undone.`;
}

// The bridge between "every row on screen" and "every row that matches". The
// table only ever holds the pages it has loaded, so ticking the header can
// never mean the whole filtered set on its own; this says what is selected and
// offers the rest in one click.
function SelectAllBanner({
    noun,
    selectAll,
    count,
    loadedCount,
    total,
    canSelectAllMatching,
    onSelectAllMatching,
    onClear,
}: {
    noun: string;
    selectAll: boolean;
    count: number;
    loadedCount: number;
    total: number;
    canSelectAllMatching: boolean;
    onSelectAllMatching: () => void;
    onClear: () => void;
}) {
    if (!selectAll && !canSelectAllMatching) return null;
    const plural = (n: number) => (n === 1 ? noun : `${noun}s`);
    return (
        <div className="px-5 py-2 bg-sky-50/70 border-b border-sky-100 text-[12px] text-sky-900 flex flex-wrap items-center justify-center gap-x-1.5 gap-y-1 text-center">
            {selectAll ? (
                <>
                    <span>
                        All <span className="font-medium">{count.toLocaleString()}</span> {plural(count)} matching this
                        view are selected.
                    </span>
                    <button
                        type="button"
                        onClick={onClear}
                        className="font-medium underline underline-offset-2 hover:text-sky-700"
                    >
                        Clear selection
                    </button>
                </>
            ) : (
                <>
                    <span>
                        The <span className="font-medium">{loadedCount.toLocaleString()}</span> {plural(loadedCount)} loaded
                        here are selected.
                    </span>
                    <button
                        type="button"
                        onClick={onSelectAllMatching}
                        className="font-medium underline underline-offset-2 hover:text-sky-700"
                    >
                        Select all {total.toLocaleString()} matching
                    </button>
                </>
            )}
        </div>
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
    onVerify,
    onMarkDeliverable,
    verifying,
    onDelete,
    onClear,
    selection,
    segment,
    onExclude,
    excluding,
    campaign,
    onRemoveFromCampaign,
    removing,
}: {
    count: number;
    deleting: boolean;
    pushTargets: IntegrationConnection[];
    pushing: boolean;
    onPush: (connectionId: string, providerLabel: string) => void;
    onBulkEdit: () => void;
    onResearch: () => void;
    researching: boolean;
    onVerify: () => void;
    onMarkDeliverable: () => void;
    verifying: boolean;
    onDelete: () => void;
    onClear: () => void;
    selection: ContactSelection;
    segment?: { id: string; name: string };
    onExclude: () => void;
    excluding: boolean;
    campaign?: MiniCampaign;
    onRemoveFromCampaign?: () => void;
    removing?: boolean;
}) {
    if (count === 0) return null;
    return (
        // Fixed, not absolute. On a campaign's Leads tab this sits inside a
        // wrapper that grows with the table, so an absolutely positioned bar
        // parked itself at the bottom of the whole list: selecting rows
        // appeared to do nothing until you scrolled past every lead.
        <div className="fixed bottom-4 left-1/2 -translate-x-1/2 z-30 flex items-center max-w-[calc(100vw-16px)] flex-wrap justify-center md:max-w-none md:flex-nowrap gap-1.5 rounded-md border border-slate-200 bg-white shadow-[0_6px_20px_-4px_rgba(15,23,42,0.12),0_2px_4px_rgba(15,23,42,0.04)] px-2 py-1.5">
            <div className="inline-flex items-center gap-1.5 px-2 h-7 rounded bg-sky-50 text-sky-700 text-[12px] font-medium">
                <CheckIcon className="w-3 h-3" />
                <span>{count.toLocaleString()} selected</span>
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
                        <PopoverMenuLabel>Push {count.toLocaleString()} to</PopoverMenuLabel>
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
            <AddToSegmentMenu selection={selection} count={count} onDone={onClear} />
            {segment && (
                <button
                    type="button"
                    onClick={onExclude}
                    disabled={excluding}
                    className="h-7 px-2.5 rounded text-[12px] text-amber-700 hover:text-white hover:bg-amber-600 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {excluding ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <XIcon className="w-3 h-3" />}
                    <span className="hidden sm:inline">Remove from segment</span>
                </button>
            )}
            {campaign && onRemoveFromCampaign && (
                <button
                    type="button"
                    onClick={onRemoveFromCampaign}
                    disabled={removing}
                    className="h-7 px-2.5 rounded text-[12px] text-amber-700 hover:text-white hover:bg-amber-600 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {removing ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <UserMinusIcon className="w-3 h-3" />}
                    <span className="hidden sm:inline">Remove from campaign</span>
                </button>
            )}
            <button
                type="button"
                onClick={onResearch}
                disabled={researching}
                className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-sky-700 hover:bg-sky-50 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
            >
                {researching ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <SparklesIcon className="w-3 h-3" />}
                <span className="hidden sm:inline">Research</span>
            </button>
            <PopoverMenu side="top" align="center">
                <PopoverMenuTrigger asChild>
                    <button
                        type="button"
                        disabled={verifying}
                        className="h-7 px-2.5 rounded text-[12px] text-slate-700 hover:text-emerald-700 hover:bg-emerald-50 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {verifying ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <ShieldCheckIcon className="w-3 h-3" />}
                        <span className="hidden sm:inline">Verify</span>
                    </button>
                </PopoverMenuTrigger>
                <PopoverMenuContent>
                    <PopoverMenuLabel>Address verification</PopoverMenuLabel>
                    <PopoverMenuItem onSelect={onVerify}>Re-verify {count.toLocaleString()}</PopoverMenuItem>
                    <PopoverMenuItem onSelect={onMarkDeliverable}>Mark deliverable</PopoverMenuItem>
                </PopoverMenuContent>
            </PopoverMenu>
            {/* Inside a campaign the destructive action is leaving the campaign,
                not leaving the workspace — same rule as the row action, which
                shows Remove instead of Delete there. */}
            {!campaign && (
                <button
                    type="button"
                    onClick={onDelete}
                    disabled={deleting}
                    className="h-7 px-2.5 rounded text-[12px] text-red-600 hover:text-white hover:bg-red-600 font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                >
                    {deleting ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <TrashIcon className="w-3 h-3" />}
                    <span className="hidden sm:inline">Delete</span>
                </button>
            )}
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
