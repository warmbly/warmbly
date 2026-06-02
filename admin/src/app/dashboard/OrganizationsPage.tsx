// Organizations browser — left filter rail + server-driven sortable,
// cursor-paged table. Searchable by identity, plan, plan visibility,
// subscription state, relationships, count ranges, and timeline date ranges.
// The Mailboxes cell links into the Mailboxes browser pre-filtered to the org.

import { useEffect, useState } from "react";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import {
    Explorer,
    FilterGroup,
    SearchFilter,
    SegmentedFilter,
    SelectFilter,
    ToggleFilter,
    DateRangeFilter,
    NumberRangeFilter,
} from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import { emptyRange, rangeActive, rangeWithin, rangeAfter, rangeBefore, type DateRange } from "@/lib/dateRange";
import { listOrganizations } from "@/lib/api/client/admin/organizations";
import { listPlans } from "@/lib/api/client/admin/plans";
import type { AdminOrgListItem } from "@/lib/api/models/admin";

type StatusFilter = "active" | "pending_deletion" | "all";
type VisibilityFilter = "" | "public" | "private" | "none";

const SUB_STATUS_OPTIONS: { value: string; label: string }[] = [
    { value: "any", label: "Any subscription" },
    { value: "trialing", label: "Trialing" },
    { value: "active", label: "Active" },
    { value: "past_due", label: "Past due" },
    { value: "canceled", label: "Canceled" },
    { value: "unpaid", label: "Unpaid" },
    { value: "incomplete", label: "Incomplete" },
    { value: "incomplete_expired", label: "Incomplete (expired)" },
    { value: "paused", label: "Paused" },
];

const VISIBILITY_OPTIONS: { value: string; label: string }[] = [
    { value: "any", label: "Any plan state" },
    { value: "public", label: "On a public plan" },
    { value: "private", label: "On a private plan" },
    { value: "none", label: "No subscription" },
];

const ownerName = (o: AdminOrgListItem) =>
    `${o.owner_first_name} ${o.owner_last_name}`.trim() || o.owner_email;

const columns: Column<AdminOrgListItem>[] = [
    {
        id: "name",
        header: "Name",
        sortable: true,
        sortKey: "name",
        cell: (o) => (
            <div>
                <Link
                    to={`/organizations/${o.id}`}
                    onClick={(e) => e.stopPropagation()}
                    className="font-medium text-[var(--admin-accent-strong)] hover:underline"
                >
                    {o.name}
                </Link>
                {o.slug && <div className="font-mono text-[10px] text-muted-foreground">{o.slug}</div>}
            </div>
        ),
        csv: (o) => o.name,
    },
    {
        id: "owner",
        header: "Owner",
        sortable: true,
        sortKey: "owner_email",
        cell: (o) => (
            <div>
                <div className="flex items-center gap-1.5">
                    <span className="text-xs">{ownerName(o)}</span>
                    {o.owner_banned_at && (
                        <Badge variant="outline" className="text-[10px] border-red-300 bg-red-50 text-red-700">
                            banned
                        </Badge>
                    )}
                </div>
                <div className="text-[10px] text-muted-foreground">{o.owner_email}</div>
            </div>
        ),
        csv: (o) => o.owner_email,
    },
    {
        id: "plan",
        header: "Plan",
        cell: (o) =>
            o.plan_name ? (
                <div className="flex items-center gap-1.5">
                    <span className="text-xs">{o.plan_name}</span>
                    {o.plan_public === false && (
                        <Badge variant="outline" className="text-[10px] border-violet-300 bg-violet-50 text-violet-700">
                            private
                        </Badge>
                    )}
                    {o.is_enterprise && (
                        <Badge variant="outline" className="text-[10px] border-indigo-300 bg-indigo-50 text-indigo-700">
                            enterprise
                        </Badge>
                    )}
                </div>
            ) : (
                <span className="text-xs text-muted-foreground">—</span>
            ),
        csv: (o) => o.plan_name || "",
    },
    { id: "members", header: "Members", align: "right", sortable: true, sortKey: "member_count", cell: (o) => <span className="tabular-nums">{o.member_count}</span>, csv: (o) => o.member_count },
    {
        id: "mailboxes",
        header: "Mailboxes",
        align: "right",
        sortable: true,
        sortKey: "email_account_count",
        cell: (o) => (
            <Link
                to={`/mailboxes?org=${o.id}`}
                onClick={(e) => e.stopPropagation()}
                className="tabular-nums text-[var(--admin-accent-strong)] hover:underline"
                title="Browse this org's mailboxes"
            >
                {o.email_account_count}
            </Link>
        ),
        csv: (o) => o.email_account_count,
    },
    {
        id: "campaigns",
        header: "Campaigns",
        align: "right",
        sortable: true,
        sortKey: "campaign_count",
        cell: (o) => (
            <span className="tabular-nums">
                {o.campaign_count}
                {o.active_campaigns > 0 && <span className="ml-1 text-emerald-600">({o.active_campaigns} active)</span>}
            </span>
        ),
        csv: (o) => o.campaign_count,
    },
    {
        id: "status",
        header: "Status",
        cell: (o) =>
            o.deletion_scheduled_for ? (
                <Badge variant="outline" className="text-[10px] border-amber-300 bg-amber-50 text-amber-700">
                    pending deletion
                </Badge>
            ) : (
                <span className="text-xs text-emerald-600">active</span>
            ),
        csv: (o) => (o.deletion_scheduled_for ? "pending_deletion" : "active"),
    },
    {
        id: "created",
        header: "Created",
        sortable: true,
        sortKey: "created_at",
        cell: (o) => <span className="text-xs text-muted-foreground">{new Date(o.created_at).toLocaleDateString()}</span>,
        csv: (o) => o.created_at,
        defaultHidden: true,
    },
];

export default function OrganizationsPage() {
    const nav = useNavigate();
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");
    const [planId, setPlanId] = useState("");
    const [visibility, setVisibility] = useState<VisibilityFilter>("");
    const [subStatus, setSubStatus] = useState("");
    const [enterprise, setEnterprise] = useState(false);
    const [hasOverrides, setHasOverrides] = useState(false);
    const [cancelAtPeriodEnd, setCancelAtPeriodEnd] = useState(false);
    const [hasActiveSubscription, setHasActiveSubscription] = useState(false);
    const [noSubscription, setNoSubscription] = useState(false);
    const [ownerBanned, setOwnerBanned] = useState(false);
    const [hasActiveCampaigns, setHasActiveCampaigns] = useState(false);
    const [hasEmailAccounts, setHasEmailAccounts] = useState(false);
    // Count ranges
    const [memMin, setMemMin] = useState<number | undefined>();
    const [memMax, setMemMax] = useState<number | undefined>();
    const [mbMin, setMbMin] = useState<number | undefined>();
    const [mbMax, setMbMax] = useState<number | undefined>();
    const [campMin, setCampMin] = useState<number | undefined>();
    const [campMax, setCampMax] = useState<number | undefined>();
    // Date ranges
    const [created, setCreated] = useState<DateRange>(emptyRange);
    const [trialEnd, setTrialEnd] = useState<DateRange>(emptyRange);
    const [periodEnd, setPeriodEnd] = useState<DateRange>(emptyRange);
    const [updated, setUpdated] = useState<DateRange>(emptyRange);

    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "", desc: true });
    const pager = useCursorPager();
    const { reset } = pager;

    const { data: plansData } = useQuery({ queryKey: ["admin", "plans", "facet"], queryFn: listPlans, staleTime: 5 * 60_000 });
    const planOptions = [
        { value: "any", label: "Any plan" },
        ...(plansData?.data ?? []).map((p) => ({ value: p.id, label: p.name || "Untitled plan" })),
    ];

    const filterKey = JSON.stringify({
        query, status, planId, visibility, subStatus, enterprise, hasOverrides, cancelAtPeriodEnd,
        hasActiveSubscription, noSubscription, ownerBanned, hasActiveCampaigns, hasEmailAccounts,
        memMin, memMax, mbMin, mbMax, campMin, campMax, created, trialEnd, periodEnd, updated, sort,
    });

    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "organizations", filterKey, pager.cursor],
        queryFn: () =>
            listOrganizations({
                q: query.trim() || undefined,
                status: status === "all" ? "" : status,
                plan_id: planId || undefined,
                plan_visibility: visibility || undefined,
                subscription_status: subStatus || undefined,
                enterprise: enterprise || undefined,
                has_overrides: hasOverrides || undefined,
                cancel_at_period_end: cancelAtPeriodEnd || undefined,
                has_active_subscription: hasActiveSubscription || undefined,
                no_subscription: noSubscription || undefined,
                owner_banned: ownerBanned || undefined,
                has_active_campaigns: hasActiveCampaigns || undefined,
                has_email_accounts: hasEmailAccounts || undefined,
                member_count_min: memMin,
                member_count_max: memMax,
                email_account_count_min: mbMin,
                email_account_count_max: mbMax,
                campaign_count_min: campMin,
                campaign_count_max: campMax,
                created_within: rangeWithin(created),
                created_after: rangeAfter(created),
                created_before: rangeBefore(created),
                trial_end_after: rangeAfter(trialEnd),
                trial_end_before: rangeBefore(trialEnd),
                current_period_end_after: rangeAfter(periodEnd),
                current_period_end_before: rangeBefore(periodEnd),
                updated_after: rangeAfter(updated),
                updated_before: rangeBefore(updated),
                limit: 50,
                cursor: pager.cursor,
                sort_by: sort.by ? (sort.by as AdminOrgSortKey) : undefined,
                sort_desc: sort.by ? sort.desc : undefined,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    const rows = data?.data ?? [];

    const bools = [enterprise, hasOverrides, cancelAtPeriodEnd, hasActiveSubscription, noSubscription, ownerBanned, hasActiveCampaigns, hasEmailAccounts];
    const ranges = [[memMin, memMax], [mbMin, mbMax], [campMin, campMax]];
    const activeCount =
        (query ? 1 : 0) +
        (status !== "active" ? 1 : 0) +
        (planId ? 1 : 0) +
        (visibility ? 1 : 0) +
        (subStatus ? 1 : 0) +
        bools.filter(Boolean).length +
        ranges.filter(([a, b]) => a !== undefined || b !== undefined).length +
        [created, trialEnd, periodEnd, updated].filter(rangeActive).length +
        (sort.by ? 1 : 0);

    function resetAll() {
        setQuery("");
        setStatus("active");
        setPlanId("");
        setVisibility("");
        setSubStatus("");
        setEnterprise(false);
        setHasOverrides(false);
        setCancelAtPeriodEnd(false);
        setHasActiveSubscription(false);
        setNoSubscription(false);
        setOwnerBanned(false);
        setHasActiveCampaigns(false);
        setHasEmailAccounts(false);
        setMemMin(undefined);
        setMemMax(undefined);
        setMbMin(undefined);
        setMbMax(undefined);
        setCampMin(undefined);
        setCampMax(undefined);
        setCreated(emptyRange);
        setTrialEnd(emptyRange);
        setPeriodEnd(emptyRange);
        setUpdated(emptyRange);
        setSort({ by: "", desc: true });
    }

    return (
        <div>
            <PageHeader title="Organizations" description="Every workspace on the platform. Filter by plan, subscription, relationships, usage, and timeline; sort and drill into owner, members, and mailboxes." />
            <Explorer
                activeCount={activeCount}
                onReset={resetAll}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Name, slug, or owner…" />
                        </FilterGroup>
                        <FilterGroup label="Status">
                            <SegmentedFilter
                                value={status}
                                onChange={setStatus}
                                options={[
                                    { value: "active", label: "Active" },
                                    { value: "pending_deletion", label: "Pending" },
                                    { value: "all", label: "All" },
                                ]}
                            />
                        </FilterGroup>
                        <FilterGroup label="Plan">
                            <SelectFilter
                                value={planId || "any"}
                                onChange={(v) => setPlanId(v === "any" ? "" : v)}
                                options={planOptions}
                                placeholder="Any plan"
                            />
                        </FilterGroup>
                        <FilterGroup label="Plan visibility">
                            <SelectFilter
                                value={visibility || "any"}
                                onChange={(v) => setVisibility(v === "any" ? "" : (v as VisibilityFilter))}
                                options={VISIBILITY_OPTIONS}
                                placeholder="Any plan state"
                            />
                        </FilterGroup>
                        <FilterGroup label="Subscription">
                            <SelectFilter
                                value={subStatus || "any"}
                                onChange={(v) => setSubStatus(v === "any" ? "" : v)}
                                options={SUB_STATUS_OPTIONS}
                                placeholder="Any subscription"
                            />
                            <div className="mt-2 flex flex-col gap-2">
                                <ToggleFilter checked={enterprise} onChange={setEnterprise} label="Enterprise plan" />
                                <ToggleFilter checked={hasActiveSubscription} onChange={setHasActiveSubscription} label="Active subscription" />
                                <ToggleFilter checked={cancelAtPeriodEnd} onChange={setCancelAtPeriodEnd} label="Canceling at period end" />
                                <ToggleFilter checked={noSubscription} onChange={setNoSubscription} label="No subscription" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Signed up">
                            <DateRangeFilter value={created} onChange={setCreated} />
                        </FilterGroup>
                        <FilterGroup label="Flags">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter checked={hasOverrides} onChange={setHasOverrides} label="Has custom limits" />
                                <ToggleFilter checked={ownerBanned} onChange={setOwnerBanned} label="Owner banned" />
                                <ToggleFilter checked={hasActiveCampaigns} onChange={setHasActiveCampaigns} label="Has active campaigns" />
                                <ToggleFilter checked={hasEmailAccounts} onChange={setHasEmailAccounts} label="Has mailboxes" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Members">
                            <NumberRangeFilter min={memMin} max={memMax} onMinChange={setMemMin} onMaxChange={setMemMax} />
                        </FilterGroup>
                        <FilterGroup label="Mailboxes">
                            <NumberRangeFilter min={mbMin} max={mbMax} onMinChange={setMbMin} onMaxChange={setMbMax} />
                        </FilterGroup>
                        <FilterGroup label="Campaigns">
                            <NumberRangeFilter min={campMin} max={campMax} onMinChange={setCampMin} onMaxChange={setCampMax} />
                        </FilterGroup>
                        <FilterGroup label="Trial ends">
                            <DateRangeFilter value={trialEnd} onChange={setTrialEnd} mode="custom" />
                        </FilterGroup>
                        <FilterGroup label="Current period ends">
                            <DateRangeFilter value={periodEnd} onChange={setPeriodEnd} mode="custom" />
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
                    getRowId={(o) => o.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load organizations"
                    onRowClick={(o) => nav(`/organizations/${o.id}`)}
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.organizations"
                    csvName="warmbly-organizations"
                    noun="organizations"
                    emptyTitle="No organizations"
                    emptyHint="No organizations match these filters."
                    pager={{
                        canPrev: pager.canPrev,
                        canNext: !!data?.pagination.has_more,
                        onPrev: pager.prev,
                        onNext: () => pager.next(data?.pagination.next_cursor),
                        page: pager.page,
                        shown: rows.length,
                        total: data?.pagination.total ?? null,
                    }}
                />
            </Explorer>
        </div>
    );
}

type AdminOrgSortKey = "created_at" | "name" | "owner_email" | "member_count" | "email_account_count" | "campaign_count";
