// Users browser — left filter rail + a server-driven, sortable, cursor-paged
// table. Searchable from every angle: identity, plan/subscription, account
// state, count ranges, and timeline (signup / admin-granted / banned / updated)
// date ranges. Row → detail.

import { useEffect, useState } from "react";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { Link, useNavigate } from "react-router-dom";
import { ShieldAlert } from "lucide-react";
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
import { searchUsers } from "@/lib/api/client/admin/users";
import { listPlans } from "@/lib/api/client/admin/plans";
import type { AdminUserDetail } from "@/lib/api/models/admin";

type StatusFilter = "active" | "banned" | "all";

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

const fullName = (u: AdminUserDetail) => `${u.first_name} ${u.last_name}`.trim() || u.email;

const columns: Column<AdminUserDetail>[] = [
    {
        id: "name",
        header: "Name",
        sortable: true,
        sortKey: "name",
        cell: (u) => (
            <div className="flex items-center gap-1.5">
                <Link
                    to={`/users/${u.id}`}
                    onClick={(e) => e.stopPropagation()}
                    className="font-medium text-[var(--admin-accent-strong)] hover:underline"
                >
                    {fullName(u)}
                </Link>
                {u.admin_permissions > 0 && (
                    <Badge
                        variant="outline"
                        className="gap-0.5 text-[10px] border-[var(--admin-accent)] text-[var(--admin-accent-strong)]"
                    >
                        <ShieldAlert className="size-2.5" />
                        admin
                    </Badge>
                )}
            </div>
        ),
        csv: (u) => fullName(u),
    },
    {
        id: "email",
        header: "Email",
        sortable: true,
        sortKey: "email",
        cell: (u) => <span className="text-muted-foreground">{u.email}</span>,
        csv: (u) => u.email,
    },
    {
        id: "status",
        header: "Status",
        cell: (u) =>
            u.banned_at ? (
                <Badge variant="outline" className="text-[10px] border-red-300 bg-red-50 text-red-700">
                    banned
                </Badge>
            ) : (
                <span className="text-xs text-emerald-600">active</span>
            ),
        csv: (u) => (u.banned_at ? "banned" : "active"),
    },
    { id: "orgs", header: "Orgs", align: "right", cell: (u) => <span className="tabular-nums">{u.organization_count}</span>, csv: (u) => u.organization_count },
    { id: "mailboxes", header: "Mailboxes", align: "right", cell: (u) => <span className="tabular-nums">{u.email_account_count}</span>, csv: (u) => u.email_account_count },
    { id: "campaigns", header: "Campaigns", align: "right", cell: (u) => <span className="tabular-nums">{u.campaign_count}</span>, csv: (u) => u.campaign_count },
    {
        id: "joined",
        header: "Joined",
        cell: (u) => <span className="text-xs text-muted-foreground">{new Date(u.created_at).toLocaleDateString()}</span>,
        csv: (u) => u.created_at,
        defaultHidden: true,
    },
];

export default function UsersPage() {
    const nav = useNavigate();
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");
    // Plan / subscription
    const [planId, setPlanId] = useState("");
    const [subStatus, setSubStatus] = useState("");
    const [isEnterprise, setIsEnterprise] = useState(false);
    const [hasSubscription, setHasSubscription] = useState(false);
    const [hasActiveSubscription, setHasActiveSubscription] = useState(false);
    // Access & state
    const [adminOnly, setAdminOnly] = useState(false);
    const [hasOverrides, setHasOverrides] = useState(false);
    const [freeTrialUsed, setFreeTrialUsed] = useState(false);
    const [onboardingCompleted, setOnboardingCompleted] = useState(false);
    const [deletionScheduled, setDeletionScheduled] = useState(false);
    const [hasAvatar, setHasAvatar] = useState(false);
    const [hasActiveCampaign, setHasActiveCampaign] = useState(false);
    const [hasBanRecord, setHasBanRecord] = useState(false);
    const [hasDedicatedWorker, setHasDedicatedWorker] = useState(false);
    // Count ranges
    const [orgMin, setOrgMin] = useState<number | undefined>();
    const [orgMax, setOrgMax] = useState<number | undefined>();
    const [mbMin, setMbMin] = useState<number | undefined>();
    const [mbMax, setMbMax] = useState<number | undefined>();
    const [campMin, setCampMin] = useState<number | undefined>();
    const [campMax, setCampMax] = useState<number | undefined>();
    const [maxOrgMin, setMaxOrgMin] = useState<number | undefined>();
    const [maxOrgMax, setMaxOrgMax] = useState<number | undefined>();
    // Date ranges
    const [created, setCreated] = useState<DateRange>(emptyRange);
    const [granted, setGranted] = useState<DateRange>(emptyRange);
    const [banned, setBanned] = useState<DateRange>(emptyRange);
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
        query, status, planId, subStatus, isEnterprise, hasSubscription, hasActiveSubscription,
        adminOnly, hasOverrides, freeTrialUsed, onboardingCompleted, deletionScheduled, hasAvatar,
        hasActiveCampaign, hasBanRecord, hasDedicatedWorker,
        orgMin, orgMax, mbMin, mbMax, campMin, campMax, maxOrgMin, maxOrgMax,
        created, granted, banned, updated, sort,
    });

    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "users", filterKey, pager.cursor],
        queryFn: () =>
            searchUsers({
                q: query.trim() || undefined,
                status: status === "all" ? "" : status,
                plan_id: planId || undefined,
                subscription_status: subStatus || undefined,
                is_enterprise: isEnterprise || undefined,
                has_subscription: hasSubscription || undefined,
                has_active_subscription: hasActiveSubscription || undefined,
                is_admin: adminOnly || undefined,
                has_overrides: hasOverrides || undefined,
                free_trial_used: freeTrialUsed || undefined,
                onboarding_completed: onboardingCompleted || undefined,
                deletion_scheduled: deletionScheduled || undefined,
                has_avatar: hasAvatar || undefined,
                has_active_campaign: hasActiveCampaign || undefined,
                has_ban_record: hasBanRecord || undefined,
                has_dedicated_worker: hasDedicatedWorker || undefined,
                org_count_min: orgMin,
                org_count_max: orgMax,
                email_account_count_min: mbMin,
                email_account_count_max: mbMax,
                campaign_count_min: campMin,
                campaign_count_max: campMax,
                max_organizations_min: maxOrgMin,
                max_organizations_max: maxOrgMax,
                created_within: rangeWithin(created),
                created_after: rangeAfter(created),
                created_before: rangeBefore(created),
                admin_granted_after: rangeAfter(granted),
                admin_granted_before: rangeBefore(granted),
                banned_after: rangeAfter(banned),
                banned_before: rangeBefore(banned),
                updated_after: rangeAfter(updated),
                updated_before: rangeBefore(updated),
                limit: 50,
                cursor: pager.cursor,
                sort_by: sort.by ? (sort.by as "name" | "email") : undefined,
                sort_desc: sort.by ? sort.desc : undefined,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    const rows = data?.data ?? [];

    const bools = [adminOnly, hasOverrides, freeTrialUsed, isEnterprise, hasSubscription, hasActiveSubscription, onboardingCompleted, deletionScheduled, hasAvatar, hasActiveCampaign, hasBanRecord, hasDedicatedWorker];
    const ranges = [[orgMin, orgMax], [mbMin, mbMax], [campMin, campMax], [maxOrgMin, maxOrgMax]];
    const activeCount =
        (query ? 1 : 0) +
        (status !== "active" ? 1 : 0) +
        (planId ? 1 : 0) +
        (subStatus ? 1 : 0) +
        bools.filter(Boolean).length +
        ranges.filter(([a, b]) => a !== undefined || b !== undefined).length +
        [created, granted, banned, updated].filter(rangeActive).length +
        (sort.by ? 1 : 0);

    function resetAll() {
        setQuery("");
        setStatus("active");
        setPlanId("");
        setSubStatus("");
        setIsEnterprise(false);
        setHasSubscription(false);
        setHasActiveSubscription(false);
        setAdminOnly(false);
        setHasOverrides(false);
        setFreeTrialUsed(false);
        setOnboardingCompleted(false);
        setDeletionScheduled(false);
        setHasAvatar(false);
        setHasActiveCampaign(false);
        setHasBanRecord(false);
        setHasDedicatedWorker(false);
        setOrgMin(undefined);
        setOrgMax(undefined);
        setMbMin(undefined);
        setMbMax(undefined);
        setCampMin(undefined);
        setCampMax(undefined);
        setMaxOrgMin(undefined);
        setMaxOrgMax(undefined);
        setCreated(emptyRange);
        setGranted(emptyRange);
        setBanned(emptyRange);
        setUpdated(emptyRange);
        setSort({ by: "", desc: true });
    }

    return (
        <div>
            <PageHeader title="Users" description="Every account on the platform. Filter by identity, plan, subscription, account state, usage, and timeline — then drill in to ban, unban, or override limits." />
            <Explorer
                activeCount={activeCount}
                onReset={resetAll}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Name or email…" />
                        </FilterGroup>
                        <FilterGroup label="Status">
                            <SegmentedFilter
                                value={status}
                                onChange={setStatus}
                                options={[
                                    { value: "active", label: "Active" },
                                    { value: "banned", label: "Banned" },
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
                        <FilterGroup label="Subscription">
                            <SelectFilter
                                value={subStatus || "any"}
                                onChange={(v) => setSubStatus(v === "any" ? "" : v)}
                                options={SUB_STATUS_OPTIONS}
                                placeholder="Any subscription"
                            />
                            <div className="mt-2 flex flex-col gap-2">
                                <ToggleFilter checked={isEnterprise} onChange={setIsEnterprise} label="Enterprise" />
                                <ToggleFilter checked={hasSubscription} onChange={setHasSubscription} label="Has a subscription" />
                                <ToggleFilter checked={hasActiveSubscription} onChange={setHasActiveSubscription} label="Active subscription" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Signed up">
                            <DateRangeFilter value={created} onChange={setCreated} />
                        </FilterGroup>
                        <FilterGroup label="Access & state">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter checked={adminOnly} onChange={setAdminOnly} label="Admins only" />
                                <ToggleFilter checked={hasOverrides} onChange={setHasOverrides} label="Has custom rate limits" />
                                <ToggleFilter checked={freeTrialUsed} onChange={setFreeTrialUsed} label="Used free trial" />
                                <ToggleFilter checked={onboardingCompleted} onChange={setOnboardingCompleted} label="Onboarding completed" />
                                <ToggleFilter checked={hasAvatar} onChange={setHasAvatar} label="Has avatar" />
                                <ToggleFilter checked={hasActiveCampaign} onChange={setHasActiveCampaign} label="Has active campaign" />
                                <ToggleFilter checked={hasDedicatedWorker} onChange={setHasDedicatedWorker} label="Has dedicated worker" />
                                <ToggleFilter checked={hasBanRecord} onChange={setHasBanRecord} label="Has ban history" />
                                <ToggleFilter checked={deletionScheduled} onChange={setDeletionScheduled} label="Deletion scheduled" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Organizations">
                            <NumberRangeFilter min={orgMin} max={orgMax} onMinChange={setOrgMin} onMaxChange={setOrgMax} />
                        </FilterGroup>
                        <FilterGroup label="Mailboxes">
                            <NumberRangeFilter min={mbMin} max={mbMax} onMinChange={setMbMin} onMaxChange={setMbMax} />
                        </FilterGroup>
                        <FilterGroup label="Campaigns">
                            <NumberRangeFilter min={campMin} max={campMax} onMinChange={setCampMin} onMaxChange={setCampMax} />
                        </FilterGroup>
                        <FilterGroup label="Org quota (max orgs)">
                            <NumberRangeFilter min={maxOrgMin} max={maxOrgMax} onMinChange={setMaxOrgMin} onMaxChange={setMaxOrgMax} />
                        </FilterGroup>
                        <FilterGroup label="Admin granted">
                            <DateRangeFilter value={granted} onChange={setGranted} mode="custom" />
                        </FilterGroup>
                        <FilterGroup label="Banned at">
                            <DateRangeFilter value={banned} onChange={setBanned} mode="custom" />
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
                    getRowId={(u) => u.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load users"
                    onRowClick={(u) => nav(`/users/${u.id}`)}
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.users"
                    csvName="warmbly-users"
                    noun="users"
                    emptyTitle="No users"
                    emptyHint="No users match these filters."
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
