// Who has actually used a code.
//
// The value columns are the snapshot taken at redemption, not the code's
// current value, so a code edited after the fact still shows what each
// workspace was really given.

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { useCursorPager } from "@/lib/useCursorPager";
import { listDiscountRedemptions } from "@/lib/api/client/admin/discounts";
import type { DiscountCode, DiscountRedemption, DiscountRedemptionStatus } from "@/lib/api/models/admin";
import { describeDiscount, formatMoney } from "./summary";

const STATUS_TONE: Record<DiscountRedemptionStatus, string> = {
    applied: "border-emerald-300 text-emerald-700 bg-emerald-50",
    pending: "border-amber-300 text-amber-700 bg-amber-50",
    canceled: "border-zinc-300 text-zinc-600 bg-zinc-50",
};

function grantedValue(r: DiscountRedemption): string {
    if (r.percent_off != null) return `${r.percent_off}% off`;
    if (r.amount_off != null) return `${formatMoney(r.amount_off, r.currency)} off`;
    if (r.trial_extension_days != null) return `${r.trial_extension_days} trial days`;
    return "—";
}

export function DiscountRedemptionsDialog({
    discount,
    open,
    onOpenChange,
}: {
    discount: DiscountCode;
    open: boolean;
    onOpenChange: (v: boolean) => void;
}) {
    const pager = useCursorPager();

    const { data, isLoading, isError } = useQuery({
        queryKey: ["admin", "discounts", discount.id, "redemptions", pager.cursor],
        queryFn: () => listDiscountRedemptions(discount.id, { cursor: pager.cursor, limit: 50 }),
        enabled: open,
        placeholderData: keepPreviousData,
    });

    const rows = data?.data ?? [];

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl">
                <DialogHeader className="shrink-0 px-4 pt-4 pb-3">
                    <DialogTitle className="flex items-center gap-2 text-[13px]">
                        <span className="font-mono">{discount.code}</span>
                        <span className="font-normal text-muted-foreground">redemptions</span>
                    </DialogTitle>
                    <DialogDescription className="text-[11px]">
                        {describeDiscount(discount)} · redeemed{" "}
                        {discount.times_redeemed.toLocaleString()}
                        {discount.max_redemptions != null
                            ? ` of ${discount.max_redemptions.toLocaleString()}`
                            : ""}{" "}
                        time{discount.times_redeemed === 1 ? "" : "s"}
                    </DialogDescription>
                </DialogHeader>

                <div className="min-h-0 flex-1 overflow-y-auto border-t border-border">
                    {isLoading ? (
                        <div className="space-y-2 p-4">
                            <Skeleton className="h-8 w-full" />
                            <Skeleton className="h-8 w-full" />
                            <Skeleton className="h-8 w-full" />
                        </div>
                    ) : isError ? (
                        <div className="p-4 text-[12.5px] text-red-600">
                            Could not load redemptions, so this list is not authoritative.
                        </div>
                    ) : rows.length === 0 ? (
                        <div className="p-4 text-[12.5px] text-muted-foreground">
                            Nobody has redeemed this code yet.
                        </div>
                    ) : (
                        <table className="w-full text-[12.5px]">
                            <thead className="sticky top-0 bg-muted/60 text-[10px] uppercase tracking-wider text-muted-foreground">
                                <tr>
                                    <th className="px-4 py-2 text-left font-semibold">Workspace</th>
                                    <th className="px-4 py-2 text-left font-semibold">Granted</th>
                                    <th className="px-4 py-2 text-left font-semibold">Status</th>
                                    <th className="px-4 py-2 text-left font-semibold">Redeemed</th>
                                </tr>
                            </thead>
                            <tbody className="divide-y divide-border">
                                {rows.map((r) => (
                                    <tr key={r.id}>
                                        <td className="px-4 py-2">
                                            <Link
                                                to={`/organizations/${r.organization_id}`}
                                                className="font-mono text-[11px] text-[var(--admin-accent-strong)] hover:underline"
                                            >
                                                {r.organization_id.slice(0, 8)}
                                            </Link>
                                        </td>
                                        <td className="px-4 py-2 tabular-nums">{grantedValue(r)}</td>
                                        <td className="px-4 py-2">
                                            <Badge
                                                variant="outline"
                                                className={`text-[10px] ${STATUS_TONE[r.status]}`}
                                            >
                                                {r.status}
                                            </Badge>
                                        </td>
                                        <td className="px-4 py-2 text-muted-foreground">
                                            {new Date(r.redeemed_at).toLocaleString()}
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    )}
                </div>

                {(pager.canPrev || data?.pagination?.has_more) && (
                    <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border px-4 py-2.5">
                        <Button
                            size="sm"
                            variant="outline"
                            className="h-7"
                            disabled={!pager.canPrev}
                            onClick={pager.prev}
                        >
                            Previous
                        </Button>
                        <Button
                            size="sm"
                            variant="outline"
                            className="h-7"
                            disabled={!data?.pagination?.has_more}
                            onClick={() => pager.next(data?.pagination?.next_cursor)}
                        >
                            Next
                        </Button>
                    </div>
                )}
            </DialogContent>
        </Dialog>
    );
}
