// Sales pipeline for inquiries submitted from the marketing site.
// Status flow: pending → contacted → converted | declined.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
    listEnterpriseInquiries,
    updateEnterpriseInquiry,
} from "@/lib/api/client/admin/enterprise";
import type {
    EnterpriseInquiry,
    EnterpriseInquiryStatus,
} from "@/lib/api/models/admin";

const STATUS_TONE: Record<EnterpriseInquiryStatus, string> = {
    pending: "border-amber-300 text-amber-700 bg-amber-50",
    contacted: "border-blue-300 text-blue-700 bg-blue-50",
    converted: "border-emerald-300 text-emerald-700 bg-emerald-50",
    declined: "border-zinc-300 text-zinc-600 bg-zinc-50",
};

const STATUSES: EnterpriseInquiryStatus[] = [
    "pending",
    "contacted",
    "converted",
    "declined",
];

type StatusFilter = EnterpriseInquiryStatus | "all";

export default function EnterprisePage() {
    const [status, setStatus] = useState<StatusFilter>("pending");
    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "enterprise", "inquiries", status],
        queryFn: () => listEnterpriseInquiries(status === "all" ? undefined : status),
        staleTime: 30_000,
    });

    const rows = data?.data ?? [];

    return (
        <div>
            <PageHeader
                title="Enterprise inquiries"
                description="Talk-to-us submissions from the marketing site. Pipeline: pending → contacted → converted or declined."
            >
                <StatusToggle value={status} onChange={setStatus} />
            </PageHeader>

            {isLoading && <Skeleton className="h-32 w-full" />}
            {error && (
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load inquiries.
                </div>
            )}

            {!isLoading && !error && rows.length === 0 && (
                <div className="text-sm text-muted-foreground border border-border rounded-md p-4 bg-card">
                    No inquiries in this status.
                </div>
            )}

            {rows.length > 0 && (
                <div className="border border-border rounded-lg overflow-hidden bg-card">
                    <table className="w-full text-sm">
                        <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                            <tr>
                                <th className="text-left px-3 py-2 font-medium">Company</th>
                                <th className="text-left px-3 py-2 font-medium">Contact</th>
                                <th className="text-right px-3 py-2 font-medium">Volume</th>
                                <th className="text-right px-3 py-2 font-medium">Team</th>
                                <th className="text-left px-3 py-2 font-medium">Notes</th>
                                <th className="text-left px-3 py-2 font-medium">Status</th>
                                <th className="text-left px-3 py-2 font-medium">Received</th>
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((i) => (
                                <InquiryRow key={i.id} inquiry={i} />
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}

function InquiryRow({ inquiry }: { inquiry: EnterpriseInquiry }) {
    const qc = useQueryClient();
    const mutation = useMutation({
        mutationFn: (next: EnterpriseInquiryStatus) =>
            updateEnterpriseInquiry(inquiry.id, { status: next }),
        onSuccess: () => {
            toast.success("Inquiry updated");
            qc.invalidateQueries({ queryKey: ["admin", "enterprise"] });
        },
        onError: (err: Error) => toast.error(err.message || "Update failed"),
    });

    return (
        <tr className="border-t border-border align-top">
            <td className="px-3 py-2">
                <div className="font-medium">{inquiry.company_name}</div>
            </td>
            <td className="px-3 py-2 text-xs">
                <div>{inquiry.contact_name}</div>
                <div className="text-muted-foreground">{inquiry.contact_email}</div>
            </td>
            <td className="px-3 py-2 text-right tabular-nums text-xs">
                {inquiry.estimated_volume != null
                    ? inquiry.estimated_volume.toLocaleString()
                    : "—"}
            </td>
            <td className="px-3 py-2 text-right tabular-nums text-xs">
                {inquiry.team_size != null ? inquiry.team_size : "—"}
            </td>
            <td className="px-3 py-2 text-xs max-w-md truncate" title={inquiry.notes}>
                {inquiry.notes || <span className="text-muted-foreground">—</span>}
            </td>
            <td className="px-3 py-2">
                <select
                    value={inquiry.status}
                    onChange={(e) =>
                        mutation.mutate(e.target.value as EnterpriseInquiryStatus)
                    }
                    disabled={mutation.isPending}
                    className={`text-[10px] px-1.5 py-1 rounded border ${
                        STATUS_TONE[inquiry.status]
                    } font-medium`}
                >
                    {STATUSES.map((s) => (
                        <option key={s} value={s}>
                            {s}
                        </option>
                    ))}
                </select>
            </td>
            <td className="px-3 py-2 text-xs text-muted-foreground">
                {new Date(inquiry.created_at).toLocaleDateString()}
            </td>
        </tr>
    );
}

function StatusToggle({
    value,
    onChange,
}: {
    value: StatusFilter;
    onChange: (v: StatusFilter) => void;
}) {
    const options: { value: StatusFilter; label: string }[] = [
        { value: "pending", label: "Pending" },
        { value: "contacted", label: "Contacted" },
        { value: "converted", label: "Converted" },
        { value: "declined", label: "Declined" },
        { value: "all", label: "All" },
    ];
    return (
        <div className="inline-flex rounded-md border border-border bg-card p-0.5 text-xs">
            {options.map((opt) => (
                <button
                    key={opt.value}
                    type="button"
                    onClick={() => onChange(opt.value)}
                    className={`px-2 py-1 rounded ${
                        value === opt.value
                            ? "bg-[var(--admin-accent)] text-white"
                            : "text-muted-foreground hover:text-foreground"
                    }`}
                >
                    {opt.label}
                </button>
            ))}
        </div>
    );
}

