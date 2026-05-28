// Users list — search over name/email, status filter (active/banned/all),
// inline admin badge and counts so abuse review is one glance. Click
// through to the detail page for ban/unban + rate-limit overrides.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { ShieldAlert } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { searchUsers } from "@/lib/api/client/admin/users";
import type { AdminUserDetail } from "@/lib/api/models/admin";

type StatusFilter = "active" | "banned" | "all";

export default function UsersPage() {
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");
    const [adminOnly, setAdminOnly] = useState(false);

    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "users", { query, status, adminOnly }],
        queryFn: () =>
            searchUsers({
                q: query.trim() || undefined,
                status: status === "all" ? "" : status,
                is_admin: adminOnly || undefined,
                limit: 50,
            }),
        staleTime: 30_000,
    });

    const rows = data?.data ?? [];
    const total = data?.pagination.total ?? rows.length;

    return (
        <div>
            <PageHeader
                title="Users"
                description="Every account on the platform. Drill in to ban, unban, override rate limits, or inspect orgs and mailboxes."
            >
                <Input
                    placeholder="Search by name or email…"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    className="w-72"
                />
                <StatusToggle value={status} onChange={setStatus} />
                <label className="inline-flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
                    <input
                        type="checkbox"
                        checked={adminOnly}
                        onChange={(e) => setAdminOnly(e.target.checked)}
                        className="accent-[var(--admin-accent)]"
                    />
                    Admins only
                </label>
            </PageHeader>

            {isLoading && <SkeletonTable />}

            {error && (
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load users. The /admin/users endpoint returned an error.
                </div>
            )}

            {!isLoading && !error && (
                <>
                    <div className="border border-border rounded-lg overflow-hidden bg-card">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                                <tr>
                                    <th className="text-left px-3 py-2 font-medium">User</th>
                                    <th className="text-left px-3 py-2 font-medium">Status</th>
                                    <th className="text-right px-3 py-2 font-medium">Orgs</th>
                                    <th className="text-right px-3 py-2 font-medium">Mailboxes</th>
                                    <th className="text-right px-3 py-2 font-medium">Campaigns</th>
                                    <th className="text-left px-3 py-2 font-medium">Joined</th>
                                </tr>
                            </thead>
                            <tbody>
                                {rows.map((u) => (
                                    <UserRow key={u.id} user={u} />
                                ))}
                                {rows.length === 0 && (
                                    <tr>
                                        <td
                                            colSpan={6}
                                            className="text-center text-muted-foreground py-8 text-sm"
                                        >
                                            No users match this filter.
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

function UserRow({ user }: { user: AdminUserDetail }) {
    const banned = !!user.banned_at;
    const isAdmin = user.admin_permissions > 0;
    const name =
        `${user.first_name} ${user.last_name}`.trim() || user.email;

    return (
        <tr className="border-t border-border hover:bg-muted/30">
            <td className="px-3 py-2">
                <div className="flex items-center gap-1.5">
                    <Link
                        to={`/users/${user.id}`}
                        className="text-[var(--admin-accent-strong)] hover:underline font-medium"
                    >
                        {name}
                    </Link>
                    {isAdmin && (
                        <Badge
                            variant="outline"
                            className="text-[10px] border-[var(--admin-accent)] text-[var(--admin-accent-strong)] bg-[color-mix(in_srgb,var(--admin-accent)_15%,transparent)]"
                        >
                            <ShieldAlert className="size-2.5" />
                            admin
                        </Badge>
                    )}
                </div>
                <div className="text-[10px] text-muted-foreground">{user.email}</div>
            </td>
            <td className="px-3 py-2">
                {banned ? (
                    <Badge
                        variant="outline"
                        className="text-[10px] border-red-300 text-red-700 bg-red-50"
                    >
                        banned
                    </Badge>
                ) : (
                    <span className="text-xs text-emerald-600">active</span>
                )}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {user.organization_count}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {user.email_account_count}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">
                {user.campaign_count}
            </td>
            <td className="px-3 py-2 text-xs text-muted-foreground">
                {new Date(user.created_at).toLocaleDateString()}
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
        { value: "banned", label: "Banned" },
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
