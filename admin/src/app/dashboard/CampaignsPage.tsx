// Platform-wide campaign admin — left filter rail + server-driven sortable,
// cursor-paged table (mirrors OrganizationsPage). Use case is finding runaway
// sends and stopping them: filter by status/tracking/behavior/usage/timeline,
// then force-stop with a reason that lands in the audit log.

import { useEffect, useState } from "react";
import {
    keepPreviousData,
    useMutation,
    useQuery,
    useQueryClient,
} from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { Octagon } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    Explorer,
    FilterGroup,
    SearchFilter,
    SelectFilter,
    ToggleFilter,
    DateRangeFilter,
    NumberRangeFilter,
} from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import {
    emptyRange,
    rangeActive,
    rangeWithin,
    rangeAfter,
    rangeBefore,
    type DateRange,
} from "@/lib/dateRange";
import { searchCampaigns, stopCampaign } from "@/lib/api/client/admin/campaigns";
import type { AdminCampaignDetail, AdminCampaignSearch } from "@/lib/api/models/admin";

const STATUS_TONE: Record<string, string> = {
    active: "border-emerald-300 text-emerald-700 bg-emerald-50",
    paused: "border-amber-300 text-amber-700 bg-amber-50",
    completed: "border-zinc-300 text-zinc-700 bg-zinc-50",
    draft: "border-zinc-300 text-zinc-500",
    paused_trial_expired: "border-orange-300 text-orange-700 bg-orange-50",
    paused_no_accounts: "border-orange-300 text-orange-700 bg-orange-50",
    paused_guardrail: "border-rose-300 text-rose-700 bg-rose-50",
};

const STATUS_OPTIONS = [
    { value: "any", label: "Any status" },
    { value: "draft", label: "Draft" },
    { value: "active", label: "Active" },
    { value: "paused", label: "Paused" },
    { value: "completed", label: "Completed" },
    { value: "paused_trial_expired", label: "Paused — trial expired" },
    { value: "paused_no_accounts", label: "Paused — no accounts" },
    { value: "paused_guardrail", label: "Paused — guardrail" },
];

const pct = (n: number, d: number) => (d ? ((n / d) * 100).toFixed(1) : "—");

export default function CampaignsPage() {
    const nav = useNavigate();
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState("");
    const [openTracking, setOpenTracking] = useState(false);
    const [linkTracking, setLinkTracking] = useState(false);
    const [stopOnReply, setStopOnReply] = useState(false);
    const [textOnly, setTextOnly] = useState(false);
    const [unsubscribeHeader, setUnsubscribeHeader] = useState(false);
    const [hasContacts, setHasContacts] = useState(false);
    const [hasBounces, setHasBounces] = useState(false);
    const [dailyMin, setDailyMin] = useState<number | undefined>();
    const [dailyMax, setDailyMax] = useState<number | undefined>();
    const [contactMin, setContactMin] = useState<number | undefined>();
    const [contactMax, setContactMax] = useState<number | undefined>();
    const [sentMin, setSentMin] = useState<number | undefined>();
    const [sentMax, setSentMax] = useState<number | undefined>();
    const [created, setCreated] = useState<DateRange>(emptyRange);
    const [startDate, setStartDate] = useState<DateRange>(emptyRange);
    const [updated, setUpdated] = useState<DateRange>(emptyRange);

    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "", desc: true });
    const pager = useCursorPager();
    const { reset } = pager;

    const [stopping, setStopping] = useState<AdminCampaignDetail | null>(null);

    const filterKey = JSON.stringify({
        query, status, openTracking, linkTracking, stopOnReply, textOnly, unsubscribeHeader,
        hasContacts, hasBounces, dailyMin, dailyMax, contactMin, contactMax, sentMin, sentMax,
        created, startDate, updated, sort,
    });

    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "campaigns", filterKey, pager.cursor],
        queryFn: () =>
            searchCampaigns({
                q: query.trim() || undefined,
                status: status || undefined,
                open_tracking: openTracking || undefined,
                link_tracking: linkTracking || undefined,
                stop_on_reply: stopOnReply || undefined,
                text_only: textOnly || undefined,
                unsubscribe_header: unsubscribeHeader || undefined,
                has_contacts: hasContacts || undefined,
                has_bounces: hasBounces || undefined,
                daily_limit_min: dailyMin,
                daily_limit_max: dailyMax,
                contact_count_min: contactMin,
                contact_count_max: contactMax,
                sent_count_min: sentMin,
                sent_count_max: sentMax,
                created_within: rangeWithin(created),
                created_after: rangeAfter(created),
                created_before: rangeBefore(created),
                start_date_after: rangeAfter(startDate),
                start_date_before: rangeBefore(startDate),
                updated_after: rangeAfter(updated),
                updated_before: rangeBefore(updated),
                limit: 50,
                cursor: pager.cursor,
                sort_by: sort.by ? (sort.by as AdminCampaignSearch["sort_by"]) : undefined,
                sort_desc: sort.by ? sort.desc : undefined,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    const rows = data?.data ?? [];

    const bools = [openTracking, linkTracking, stopOnReply, textOnly, unsubscribeHeader, hasContacts, hasBounces];
    const ranges = [[dailyMin, dailyMax], [contactMin, contactMax], [sentMin, sentMax]];
    const activeCount =
        (query ? 1 : 0) +
        (status ? 1 : 0) +
        bools.filter(Boolean).length +
        ranges.filter(([a, b]) => a !== undefined || b !== undefined).length +
        [created, startDate, updated].filter(rangeActive).length +
        (sort.by ? 1 : 0);

    function resetAll() {
        setQuery("");
        setStatus("");
        setOpenTracking(false);
        setLinkTracking(false);
        setStopOnReply(false);
        setTextOnly(false);
        setUnsubscribeHeader(false);
        setHasContacts(false);
        setHasBounces(false);
        setDailyMin(undefined);
        setDailyMax(undefined);
        setContactMin(undefined);
        setContactMax(undefined);
        setSentMin(undefined);
        setSentMax(undefined);
        setCreated(emptyRange);
        setStartDate(emptyRange);
        setUpdated(emptyRange);
        setSort({ by: "", desc: true });
    }

    const columns: Column<AdminCampaignDetail>[] = [
        {
            id: "name",
            header: "Name",
            sortable: true,
            sortKey: "name",
            cell: (c) => (
                <div>
                    <Link
                        to={`/campaigns/${c.id}`}
                        onClick={(e) => e.stopPropagation()}
                        className="font-medium text-[var(--admin-accent-strong)] hover:underline"
                    >
                        {c.name}
                    </Link>
                    <div className="font-mono text-[10px] text-muted-foreground">{c.id.slice(0, 8)}</div>
                </div>
            ),
            csv: (c) => c.name,
        },
        {
            id: "owner",
            header: "Owner",
            sortable: true,
            sortKey: "owner_email",
            cell: (c) => (
                <span className="text-xs">{c.user?.email ?? c.user_id.slice(0, 8)}</span>
            ),
            csv: (c) => c.user?.email ?? c.user_id,
        },
        {
            id: "organization",
            header: "Organization",
            cell: (c) =>
                c.organization ? (
                    <Link
                        to={`/organizations/${c.organization_id}`}
                        onClick={(e) => e.stopPropagation()}
                        className="text-xs text-[var(--admin-accent-strong)] hover:underline"
                    >
                        {c.organization.name}
                    </Link>
                ) : (
                    <span className="text-xs text-muted-foreground">—</span>
                ),
            csv: (c) => c.organization?.name ?? "",
        },
        {
            id: "status",
            header: "Status",
            sortable: true,
            sortKey: "status",
            cell: (c) => (
                <Badge variant="outline" className={`text-[10px] ${STATUS_TONE[c.status] ?? "border-zinc-300 text-zinc-600"}`}>
                    {c.status}
                </Badge>
            ),
            csv: (c) => c.status,
        },
        { id: "contacts", header: "Contacts", align: "right", sortable: true, sortKey: "contact_count", cell: (c) => <span className="tabular-nums">{c.total_contacts.toLocaleString()}</span>, csv: (c) => c.total_contacts },
        { id: "sent", header: "Sent", align: "right", sortable: true, sortKey: "sent_count", cell: (c) => <span className="tabular-nums">{c.emails_sent.toLocaleString()}</span>, csv: (c) => c.emails_sent },
        { id: "open", header: "Open", align: "right", cell: (c) => <span className="tabular-nums text-muted-foreground">{c.emails_opened.toLocaleString()}</span>, csv: (c) => c.emails_opened },
        { id: "reply", header: "Reply", align: "right", cell: (c) => <span className="tabular-nums text-muted-foreground">{pct(c.emails_replied, c.emails_sent)}%</span>, csv: (c) => pct(c.emails_replied, c.emails_sent) },
        {
            id: "bounce",
            header: "Bounce",
            align: "right",
            cell: (c) => {
                const rate = pct(c.emails_bounced, c.emails_sent);
                return (
                    <span className={`tabular-nums ${Number(rate) > 5 ? "text-red-700" : "text-muted-foreground"}`}>{rate}%</span>
                );
            },
            csv: (c) => pct(c.emails_bounced, c.emails_sent),
        },
        { id: "created", header: "Created", sortable: true, sortKey: "created_at", defaultHidden: true, cell: (c) => <span className="text-xs text-muted-foreground">{new Date(c.created_at).toLocaleDateString()}</span>, csv: (c) => c.created_at },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (c) => {
                const canStop = c.status === "active" || c.status === "paused";
                return (
                    <Button
                        size="sm"
                        variant="outline"
                        disabled={!canStop}
                        onClick={(e) => {
                            e.stopPropagation();
                            setStopping(c);
                        }}
                        className="text-xs"
                        title={canStop ? "Force-stop this campaign" : "Not running"}
                    >
                        <Octagon className="size-3" /> Stop
                    </Button>
                );
            },
        },
    ];

    return (
        <div>
            <PageHeader
                title="Campaigns"
                description="Every campaign on the platform. Filter by status, tracking, behavior, usage, and timeline; inspect engagement and force-stop runaway sends."
            />
            <Explorer
                activeCount={activeCount}
                onReset={resetAll}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Name or owner email…" />
                        </FilterGroup>
                        <FilterGroup label="Status">
                            <SelectFilter
                                value={status || "any"}
                                onChange={(v) => setStatus(v === "any" ? "" : v)}
                                options={STATUS_OPTIONS}
                                placeholder="Any status"
                            />
                        </FilterGroup>
                        <FilterGroup label="Created">
                            <DateRangeFilter value={created} onChange={setCreated} />
                        </FilterGroup>
                        <FilterGroup label="Tracking">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter checked={openTracking} onChange={setOpenTracking} label="Open tracking" />
                                <ToggleFilter checked={linkTracking} onChange={setLinkTracking} label="Link tracking" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Behavior">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter checked={stopOnReply} onChange={setStopOnReply} label="Stop on reply" />
                                <ToggleFilter checked={textOnly} onChange={setTextOnly} label="Text only" />
                                <ToggleFilter checked={unsubscribeHeader} onChange={setUnsubscribeHeader} label="Unsubscribe header" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Flags">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter checked={hasContacts} onChange={setHasContacts} label="Has contacts" />
                                <ToggleFilter checked={hasBounces} onChange={setHasBounces} label="Has bounces" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Daily limit">
                            <NumberRangeFilter min={dailyMin} max={dailyMax} onMinChange={setDailyMin} onMaxChange={setDailyMax} />
                        </FilterGroup>
                        <FilterGroup label="Contacts">
                            <NumberRangeFilter min={contactMin} max={contactMax} onMinChange={setContactMin} onMaxChange={setContactMax} />
                        </FilterGroup>
                        <FilterGroup label="Emails sent">
                            <NumberRangeFilter min={sentMin} max={sentMax} onMinChange={setSentMin} onMaxChange={setSentMax} />
                        </FilterGroup>
                        <FilterGroup label="Starts">
                            <DateRangeFilter value={startDate} onChange={setStartDate} mode="custom" />
                        </FilterGroup>
                        <FilterGroup label="Last updated">
                            <DateRangeFilter value={updated} onChange={setUpdated} mode="custom" />
                        </FilterGroup>
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(c) => c.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load campaigns"
                    onRowClick={(c) => nav(`/campaigns/${c.id}`)}
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.campaigns"
                    csvName="warmbly-campaigns"
                    noun="campaigns"
                    emptyTitle="No campaigns"
                    emptyHint="No campaigns match these filters."
                    pager={{
                        canPrev: pager.canPrev,
                        canNext: !!data?.pagination?.has_more,
                        onPrev: pager.prev,
                        onNext: () => pager.next(data?.pagination?.next_cursor),
                        page: pager.page,
                        shown: rows.length,
                        total: data?.pagination?.total ?? null,
                    }}
                />
            </Explorer>

            {stopping && (
                <StopCampaignDialog
                    campaign={stopping}
                    open
                    onOpenChange={(v) => !v && setStopping(null)}
                />
            )}
        </div>
    );
}

function StopCampaignDialog({
    campaign,
    open,
    onOpenChange,
}: {
    campaign: AdminCampaignDetail;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const qc = useQueryClient();
    const [reason, setReason] = useState("");
    const mutation = useMutation({
        mutationFn: () => stopCampaign(campaign.id, reason),
        onSuccess: () => {
            toast.success("Campaign stopped");
            qc.invalidateQueries({ queryKey: ["admin", "campaigns"] });
            onOpenChange(false);
        },
        onError: (err: Error) => toast.error(err.message || "Failed to stop"),
    });

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>Force-stop campaign</DialogTitle>
                    <DialogDescription>
                        Stopping <span className="font-mono">{campaign.name}</span>.
                        This is logged to the audit trail and the campaign owner
                        will see the campaign status change to stopped.
                    </DialogDescription>
                </DialogHeader>
                <div>
                    <Label htmlFor="reason" className="text-xs font-medium">
                        Reason
                    </Label>
                    <Input
                        id="reason"
                        placeholder="e.g. exceeded bounce threshold, unsolicited list"
                        value={reason}
                        onChange={(e) => setReason(e.target.value)}
                        autoFocus
                    />
                </div>
                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)}>
                        Cancel
                    </Button>
                    <Button
                        onClick={() => {
                            if (reason.trim() === "") {
                                toast.error("Reason is required");
                                return;
                            }
                            mutation.mutate();
                        }}
                        disabled={mutation.isPending}
                        className="bg-red-600 hover:bg-red-700 text-white"
                    >
                        {mutation.isPending ? "Stopping…" : "Stop campaign"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}
