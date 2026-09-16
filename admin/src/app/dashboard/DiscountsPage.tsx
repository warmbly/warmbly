// Promo codes — filter rail + server-driven sortable, cursor-paged table,
// mirroring the other admin browsers.
//
// This is the operator half of the discount system. The customer half (the
// promo field on the billing page, validation, and the one-off Stripe coupon
// minted per redemption at checkout) has always existed; before this page the
// only way to create a code was an INSERT against the database.

import { useEffect, useState } from "react";
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Pencil, Plus, Trash2, Users } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    DateRangeFilter,
    Explorer,
    FilterGroup,
    NumberRangeFilter,
    SearchFilter,
    SelectFilter,
    ToggleFilter,
} from "@/components/data/Explorer";
import { DataTable, type Column } from "@/components/data/DataTable";
import { useCursorPager } from "@/lib/useCursorPager";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { AdminPerm } from "@/lib/auth/permissions";
import {
    emptyRange,
    rangeActive,
    rangeAfter,
    rangeBefore,
    rangeWithin,
    type DateRange,
} from "@/lib/dateRange";
import { deleteDiscount, listDiscounts } from "@/lib/api/client/admin/discounts";
import { listAdminPlans } from "@/lib/api/client/admin/organizations";
import type { AdminDiscountSearch, DiscountCode } from "@/lib/api/models/admin";
import { DiscountBuilderDialog } from "./discounts/DiscountBuilderDialog";
import { DiscountRedemptionsDialog } from "./discounts/DiscountRedemptionsDialog";
import { describeDiscount, describeScope, effectiveState } from "./discounts/summary";

const STATUS_OPTIONS = [
    { value: "any", label: "Any status" },
    { value: "active", label: "Active" },
    { value: "disabled", label: "Disabled" },
    { value: "expired", label: "Expired" },
];

const TYPE_OPTIONS = [
    { value: "any", label: "Any type" },
    { value: "percent", label: "Percentage off" },
    { value: "fixed", label: "Fixed amount" },
    { value: "trial_extension", label: "Free trial days" },
];

const DURATION_OPTIONS = [
    { value: "any", label: "Any duration" },
    { value: "once", label: "First invoice" },
    { value: "repeating", label: "Multiple months" },
    { value: "forever", label: "Forever" },
];

const SCOPE_OPTIONS = [
    { value: "any", label: "Any plans" },
    { value: "all", label: "All plans" },
    { value: "specific", label: "Specific plans" },
];

function fmtDate(ts?: string | null) {
    if (!ts) return "—";
    return new Date(ts).toLocaleDateString(undefined, {
        year: "numeric",
        month: "short",
        day: "numeric",
    });
}

export default function DiscountsPage() {
    const qc = useQueryClient();
    const canManage = useAdminPerm(AdminPerm.ManageOrganizations);

    const [query, setQuery] = useState("");
    const [status, setStatus] = useState("");
    const [type, setType] = useState("");
    const [duration, setDuration] = useState("");
    const [scope, setScope] = useState("");
    const [hasRedemptions, setHasRedemptions] = useState(false);
    const [exhausted, setExhausted] = useState(false);
    const [hasExpiry, setHasExpiry] = useState(false);
    const [redMin, setRedMin] = useState<number | undefined>();
    const [redMax, setRedMax] = useState<number | undefined>();
    const [pctMin, setPctMin] = useState<number | undefined>();
    const [pctMax, setPctMax] = useState<number | undefined>();
    const [created, setCreated] = useState<DateRange>(emptyRange);
    const [expires, setExpires] = useState<DateRange>(emptyRange);

    const [sort, setSort] = useState<{ by: string; desc: boolean }>({ by: "", desc: true });
    const pager = useCursorPager();
    const { reset } = pager;

    const [builder, setBuilder] = useState<{ existing: DiscountCode | null } | null>(null);
    const [viewing, setViewing] = useState<DiscountCode | null>(null);
    const [deleting, setDeleting] = useState<DiscountCode | null>(null);

    const filterKey = JSON.stringify({
        query, status, type, duration, scope, hasRedemptions, exhausted, hasExpiry,
        redMin, redMax, pctMin, pctMax, created, expires, sort,
    });

    useEffect(() => {
        reset();
    }, [filterKey, reset]);

    const { data, isLoading, error, refetch } = useQuery({
        queryKey: ["admin", "discounts", filterKey, pager.cursor],
        queryFn: () =>
            listDiscounts({
                search: query.trim() || undefined,
                status: (status || undefined) as AdminDiscountSearch["status"],
                type: (type || undefined) as AdminDiscountSearch["type"],
                duration: (duration || undefined) as AdminDiscountSearch["duration"],
                plan_scope: (scope || undefined) as AdminDiscountSearch["plan_scope"],
                has_redemptions: hasRedemptions || undefined,
                exhausted: exhausted || undefined,
                has_expiry: hasExpiry || undefined,
                times_redeemed_min: redMin,
                times_redeemed_max: redMax,
                percent_off_min: pctMin,
                percent_off_max: pctMax,
                created_within: rangeWithin(created),
                created_after: rangeAfter(created),
                created_before: rangeBefore(created),
                expires_after: rangeAfter(expires),
                expires_before: rangeBefore(expires),
                limit: 50,
                cursor: pager.cursor,
                sort_by: sort.by ? (sort.by as AdminDiscountSearch["sort_by"]) : undefined,
                sort_desc: sort.by ? sort.desc : undefined,
            }),
        staleTime: 30_000,
        placeholderData: keepPreviousData,
    });

    // Names for the plan-scope column. A code restricted to plans shows what
    // they are, not a row of uuids.
    const plans = useQuery({
        queryKey: ["admin", "plans"],
        queryFn: () => listAdminPlans(),
        staleTime: 5 * 60_000,
    });
    const planNames = new Map((plans.data?.plans ?? []).map((p) => [p.id, p.name ?? p.id]));

    const remove = useMutation({
        mutationFn: (id: string) => deleteDiscount(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["admin", "discounts"] });
            toast.success("Code deleted");
            setDeleting(null);
        },
        onError: (e: Error) => toast.error(e.message || "Could not delete the code"),
    });

    const rows = data?.data ?? [];

    const bools = [hasRedemptions, exhausted, hasExpiry];
    const ranges = [[redMin, redMax], [pctMin, pctMax]];
    const activeCount =
        (query ? 1 : 0) +
        (status ? 1 : 0) +
        (type ? 1 : 0) +
        (duration ? 1 : 0) +
        (scope ? 1 : 0) +
        bools.filter(Boolean).length +
        ranges.filter(([a, b]) => a !== undefined || b !== undefined).length +
        [created, expires].filter(rangeActive).length +
        (sort.by ? 1 : 0);

    function resetAll() {
        setQuery("");
        setStatus("");
        setType("");
        setDuration("");
        setScope("");
        setHasRedemptions(false);
        setExhausted(false);
        setHasExpiry(false);
        setRedMin(undefined);
        setRedMax(undefined);
        setPctMin(undefined);
        setPctMax(undefined);
        setCreated(emptyRange);
        setExpires(emptyRange);
        setSort({ by: "", desc: true });
    }

    const columns: Column<DiscountCode>[] = [
        {
            id: "code",
            header: "Code",
            sortable: true,
            sortKey: "code",
            cell: (r) => (
                <div>
                    <div className="font-mono font-medium">{r.code}</div>
                    {r.description && (
                        <div className="max-w-xs truncate text-[10px] text-muted-foreground" title={r.description}>
                            {r.description}
                        </div>
                    )}
                </div>
            ),
            csv: (r) => r.code,
        },
        {
            id: "discount",
            header: "Discount",
            cell: (r) => <span className="text-xs">{describeDiscount(r)}</span>,
            csv: (r) => describeDiscount(r),
        },
        {
            id: "plans",
            header: "Plans",
            cell: (r) => (
                <span className="text-xs text-muted-foreground">{describeScope(r, planNames)}</span>
            ),
            csv: (r) => describeScope(r, planNames),
        },
        {
            id: "redeemed",
            header: "Redeemed",
            align: "right",
            sortable: true,
            sortKey: "times_redeemed",
            cell: (r) => (
                <span className="tabular-nums">
                    {r.times_redeemed.toLocaleString()}
                    <span className="text-[10px] text-muted-foreground">
                        {r.max_redemptions != null ? ` / ${r.max_redemptions.toLocaleString()}` : " / ∞"}
                    </span>
                </span>
            ),
            csv: (r) => r.times_redeemed,
        },
        {
            id: "per_account",
            header: "Per workspace",
            align: "right",
            defaultHidden: true,
            cell: (r) => <span className="tabular-nums text-muted-foreground">{r.per_account_limit}</span>,
            csv: (r) => r.per_account_limit,
        },
        {
            id: "state",
            header: "State",
            sortable: true,
            sortKey: "status",
            cell: (r) => {
                const s = effectiveState(r);
                return (
                    <Badge variant="outline" className={`text-[10px] ${s.tone}`}>
                        {s.label}
                    </Badge>
                );
            },
            csv: (r) => effectiveState(r).label,
        },
        {
            id: "starts",
            header: "Starts",
            sortable: true,
            sortKey: "starts_at",
            defaultHidden: true,
            cell: (r) => <span className="text-xs text-muted-foreground">{fmtDate(r.starts_at)}</span>,
            csv: (r) => r.starts_at ?? "",
        },
        {
            id: "expires",
            header: "Expires",
            sortable: true,
            sortKey: "expires_at",
            cell: (r) => <span className="text-xs text-muted-foreground">{fmtDate(r.expires_at)}</span>,
            csv: (r) => r.expires_at ?? "",
        },
        {
            id: "created",
            header: "Created",
            sortable: true,
            sortKey: "created_at",
            defaultHidden: true,
            cell: (r) => <span className="text-xs text-muted-foreground">{fmtDate(r.created_at)}</span>,
            csv: (r) => r.created_at,
        },
        {
            id: "actions",
            header: "",
            align: "right",
            cell: (r) => (
                <div className="flex justify-end gap-1 whitespace-nowrap">
                    <Button
                        size="sm"
                        variant="outline"
                        className="h-7"
                        onClick={(e) => {
                            e.stopPropagation();
                            setViewing(r);
                        }}
                        title="Redemptions"
                    >
                        <Users className="size-3" />
                    </Button>
                    {canManage && (
                        <>
                            <Button
                                size="sm"
                                variant="outline"
                                className="h-7"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    setBuilder({ existing: r });
                                }}
                                title="Edit"
                            >
                                <Pencil className="size-3" />
                            </Button>
                            <Button
                                size="sm"
                                variant="outline"
                                className="h-7 text-red-600 hover:bg-red-50"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    setDeleting(r);
                                }}
                                title="Delete"
                            >
                                <Trash2 className="size-3" />
                            </Button>
                        </>
                    )}
                </div>
            ),
        },
    ];

    return (
        <div>
            <PageHeader
                title="Promo codes"
                description="Codes customers type at checkout or on their billing page. Warmbly validates the code and mints a matching one-off Stripe coupon per redemption, so codes are never created in the Stripe dashboard."
            >
                {canManage && (
                    <Button size="sm" className="h-8" onClick={() => setBuilder({ existing: null })}>
                        <Plus className="size-3.5" /> New code
                    </Button>
                )}
            </PageHeader>
            <Explorer
                activeCount={activeCount}
                onReset={resetAll}
                filters={
                    <>
                        <FilterGroup label="Search">
                            <SearchFilter value={query} onChange={setQuery} placeholder="Code or note…" />
                        </FilterGroup>
                        <FilterGroup label="Status">
                            <SelectFilter
                                value={status || "any"}
                                onChange={(v) => setStatus(v === "any" ? "" : v)}
                                options={STATUS_OPTIONS}
                                placeholder="Any status"
                            />
                        </FilterGroup>
                        <FilterGroup label="Type">
                            <SelectFilter
                                value={type || "any"}
                                onChange={(v) => setType(v === "any" ? "" : v)}
                                options={TYPE_OPTIONS}
                                placeholder="Any type"
                            />
                        </FilterGroup>
                        <FilterGroup label="Duration">
                            <SelectFilter
                                value={duration || "any"}
                                onChange={(v) => setDuration(v === "any" ? "" : v)}
                                options={DURATION_OPTIONS}
                                placeholder="Any duration"
                            />
                        </FilterGroup>
                        <FilterGroup label="Plan scope">
                            <SelectFilter
                                value={scope || "any"}
                                onChange={(v) => setScope(v === "any" ? "" : v)}
                                options={SCOPE_OPTIONS}
                                placeholder="Any plans"
                            />
                        </FilterGroup>
                        <FilterGroup label="Usage">
                            <div className="flex flex-col gap-2">
                                <ToggleFilter
                                    checked={hasRedemptions}
                                    onChange={setHasRedemptions}
                                    label="Has redemptions"
                                />
                                <ToggleFilter checked={exhausted} onChange={setExhausted} label="Exhausted" />
                                <ToggleFilter checked={hasExpiry} onChange={setHasExpiry} label="Has an expiry" />
                            </div>
                        </FilterGroup>
                        <FilterGroup label="Times redeemed">
                            <NumberRangeFilter
                                min={redMin}
                                max={redMax}
                                onMinChange={setRedMin}
                                onMaxChange={setRedMax}
                            />
                        </FilterGroup>
                        <FilterGroup label="Percentage off">
                            <NumberRangeFilter
                                min={pctMin}
                                max={pctMax}
                                onMinChange={setPctMin}
                                onMaxChange={setPctMax}
                            />
                        </FilterGroup>
                        <FilterGroup label="Created">
                            <DateRangeFilter value={created} onChange={setCreated} />
                        </FilterGroup>
                        <FilterGroup label="Expires">
                            <DateRangeFilter value={expires} onChange={setExpires} mode="custom" />
                        </FilterGroup>
                    </>
                }
            >
                <DataTable
                    columns={columns}
                    rows={rows}
                    getRowId={(r) => r.id}
                    loading={isLoading}
                    error={error}
                    onRetry={() => refetch()}
                    errorTitle="Failed to load promo codes"
                    onRowClick={(r) => setViewing(r)}
                    sort={sort.by ? sort : undefined}
                    onSortChange={setSort}
                    storageKey="admin.discounts"
                    csvName="warmbly-promo-codes"
                    noun="codes"
                    emptyTitle="No promo codes"
                    emptyHint={
                        activeCount > 0
                            ? "No codes match these filters."
                            : "Nothing has been created yet. A code here is what a customer types at checkout."
                    }
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

            {builder && (
                <DiscountBuilderDialog
                    existing={builder.existing}
                    open
                    onOpenChange={(v) => !v && setBuilder(null)}
                />
            )}

            {viewing && (
                <DiscountRedemptionsDialog
                    discount={viewing}
                    open
                    onOpenChange={(v) => !v && setViewing(null)}
                />
            )}

            {deleting && (
                <Dialog open onOpenChange={(v) => !v && setDeleting(null)}>
                    <DialogContent className="sm:max-w-md">
                        <DialogHeader>
                            <DialogTitle className="text-[13px]">Delete {deleting.code}?</DialogTitle>
                            <DialogDescription className="text-[11px] leading-relaxed">
                                Nobody will be able to redeem it again. Workspaces that already redeemed it
                                keep their discount: the Stripe coupon was minted per redemption and is not
                                affected. Disable the code instead if you only want to stop new redemptions
                                but keep the history readable under its name.
                            </DialogDescription>
                        </DialogHeader>
                        <DialogFooter>
                            <Button
                                size="sm"
                                variant="outline"
                                className="h-8"
                                onClick={() => setDeleting(null)}
                                disabled={remove.isPending}
                            >
                                Cancel
                            </Button>
                            <Button
                                size="sm"
                                className="h-8 bg-red-600 text-white hover:bg-red-700"
                                disabled={remove.isPending}
                                onClick={() => remove.mutate(deleting.id)}
                            >
                                {remove.isPending ? "Deleting…" : "Delete"}
                            </Button>
                        </DialogFooter>
                    </DialogContent>
                </Dialog>
            )}
        </div>
    );
}
