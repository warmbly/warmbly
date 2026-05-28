// Organizations list — table view over /admin/organizations. Search
// covers org name/slug and owner email; status filter splits active from
// soft-deleted. Per-row counts come inline from the backend so the table
// can render usage without a fan-out fetch.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { listOrganizations } from "@/lib/api/client/admin/organizations";
import type { AdminOrgListItem } from "@/lib/api/models/admin";

type StatusFilter = "active" | "pending_deletion" | "all";

export default function OrganizationsPage() {
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");

    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "organizations", { query, status }],
        queryFn: () =>
            listOrganizations({
                q: query.trim() || undefined,
                status: status === "all" ? "" : status,
                limit: 50,
            }),
        // Counts move slowly; 30s is plenty for an ops surface.
        staleTime: 30_000,
    });

    const rows = data?.data ?? [];
    const total = data?.pagination.total ?? rows.length;

    return (
        <div>
            <PageHeader
                title="Organizations"
                description="Every workspace on the platform. Owner, plan, and live resource usage at a glance."
            >
                <Input
                    placeholder="Search by name, slug, or owner email…"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    className="w-80"
                />
                <StatusToggle value={status} onChange={setStatus} />
            </PageHeader>

            {isLoading && <SkeletonTable />}

            {error && (
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load organizations. The /admin/organizations
                    endpoint returned an error.
                </div>
            )}

            {!isLoading && !error && (
                <>
                    <div className="border border-border rounded-lg overflow-hidden bg-card">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                                <tr>
                                    <th className="text-left px-3 py-2 font-medium">Name</th>
                                    <th className="text-left px-3 py-2 font-medium">Owner</th>
                                    <th className="text-right px-3 py-2 font-medium">Members</th>
                                    <th className="text-right px-3 py-2 font-medium">Mailboxes</th>
                                    <th className="text-right px-3 py-2 font-medium">Campaigns</th>
                                    <th className="text-left px-3 py-2 font-medium">Status</th>
                                    <th className="text-left px-3 py-2 font-medium">Created</th>
                                </tr>
                            </thead>
                            <tbody>
                                {rows.map((o) => (
                                    <Row key={o.id} org={o} />
                                ))}
                                {rows.length === 0 && (
                                    <tr>
                                        <td
                                            colSpan={7}
                                            className="text-center text-muted-foreground py-8 text-sm"
                                        >
                                            No organizations match this filter.
                                        </td>
                                    </tr>
                                )}
                            </tbody>
                        </table>
                    </div>

                    <div className="mt-3 flex items-center justify-between text-xs text-muted-foreground">
                        <span>
                            Showing {rows.length}
                            {total != null && total !== rows.length && (
                                <> of {total.toLocaleString()}</>
                            )}
                        </span>
                        {data?.pagination.has_more && (
                            <span>
                                More results available — refine the search to narrow.
                            </span>
                        )}
                    </div>
                </>
            )}
        </div>
    );
}

function Row({ org }: { org: AdminOrgListItem }) {
    const pending = !!org.deletion_scheduled_for;
    const ownerBanned = !!org.owner_banned_at;
    const ownerName =
        `${org.owner_first_name} ${org.owner_last_name}`.trim() ||
        org.owner_email;

    return (
        <tr className="border-t border-border hover:bg-muted/30">
            <td className="px-3 py-2">
                <Link
                    to={`/organizations/${org.id}`}
                    className="text-[var(--admin-accent-strong)] hover:underline font-medium"
                >
                    {org.name}
                </Link>
                {org.slug && (
                    <div className="text-[10px] text-muted-foreground font-mono">
                        {org.slug}
                    </div>
                )}
            </td>
            <td className="px-3 py-2">
                <div className="flex items-center gap-1.5">
                    <span className="text-xs">{ownerName}</span>
                    {ownerBanned && (
                        <Badge
                            variant="outline"
                            className="text-[10px] border-red-300 text-red-700 bg-red-50"
                        >
                            banned
                        </Badge>
                    )}
                </div>
                <div className="text-[10px] text-muted-foreground">
                    {org.owner_email}
                </div>
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {org.member_count}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {org.email_account_count}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {org.campaign_count}
                {org.active_campaigns > 0 && (
                    <span className="text-emerald-600 ml-1">
                        ({org.active_campaigns} active)
                    </span>
                )}
            </td>
            <td className="px-3 py-2">
                {pending ? (
                    <Badge
                        variant="outline"
                        className="text-[10px] border-amber-300 text-amber-700 bg-amber-50"
                    >
                        pending deletion
                    </Badge>
                ) : (
                    <span className="text-xs text-emerald-600">active</span>
                )}
            </td>
            <td className="px-3 py-2 text-xs text-muted-foreground">
                {new Date(org.created_at).toLocaleDateString()}
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
        { value: "active", label: "Active" },
        { value: "pending_deletion", label: "Pending deletion" },
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

function SkeletonTable() {
    return (
        <div className="border border-border rounded-lg p-4 bg-card space-y-2">
            {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-7 w-full" />
            ))}
        </div>
    );
}
