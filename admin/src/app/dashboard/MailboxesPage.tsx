// Cross-org mailbox triage. Use case: "find every Gmail mailbox that
// hasn't synced in 24h" or "show me which mailboxes belong to
// acme.com's workspace before quarantining". Filters: search,
// active/inactive, provider.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { searchMailboxes } from "@/lib/api/client/admin/mailboxes";
import type { AdminMailboxRow } from "@/lib/api/models/admin";

type StatusFilter = "active" | "inactive" | "all";

const STALE_MS = 24 * 60 * 60 * 1000;

export default function MailboxesPage() {
    const [query, setQuery] = useState("");
    const [status, setStatus] = useState<StatusFilter>("active");
    const [provider, setProvider] = useState("");

    const { data, isLoading, error } = useQuery({
        queryKey: ["admin", "mailboxes", { query, status, provider }],
        queryFn: () =>
            searchMailboxes({
                q: query.trim() || undefined,
                status,
                provider: provider || undefined,
                limit: 50,
            }),
        staleTime: 30_000,
    });

    const rows = data?.data ?? [];
    const total = data?.pagination.total ?? rows.length;

    return (
        <div>
            <PageHeader
                title="Mailboxes"
                description="Every connected mailbox across all workspaces. Health, warmup state, last sync, plus the workspace and owner it belongs to."
            >
                <Input
                    placeholder="Search by email, owner, or org…"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    className="w-72"
                />
                <ProviderToggle value={provider} onChange={setProvider} />
                <StatusToggle value={status} onChange={setStatus} />
            </PageHeader>

            {isLoading && <SkeletonTable />}
            {error && (
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load mailboxes.
                </div>
            )}

            {!isLoading && !error && (
                <>
                    <div className="border border-border rounded-lg overflow-hidden bg-card">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                                <tr>
                                    <th className="text-left px-3 py-2 font-medium">Mailbox</th>
                                    <th className="text-left px-3 py-2 font-medium">Owner / org</th>
                                    <th className="text-left px-3 py-2 font-medium">Provider</th>
                                    <th className="text-left px-3 py-2 font-medium">Status</th>
                                    <th className="text-left px-3 py-2 font-medium">Warmup</th>
                                    <th className="text-right px-3 py-2 font-medium">Cap</th>
                                    <th className="text-left px-3 py-2 font-medium">Last sync</th>
                                </tr>
                            </thead>
                            <tbody>
                                {rows.map((m) => (
                                    <MailboxRow key={m.id} m={m} />
                                ))}
                                {rows.length === 0 && (
                                    <tr>
                                        <td colSpan={7} className="text-center text-muted-foreground py-8 text-sm">
                                            No mailboxes match this filter.
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
                            <span>More results available — refine the search.</span>
                        )}
                    </div>
                </>
            )}
        </div>
    );
}

function MailboxRow({ m }: { m: AdminMailboxRow }) {
    const stale =
        m.last_synced_at &&
        Date.now() - new Date(m.last_synced_at).getTime() > STALE_MS;
    const never = !m.last_synced_at;
    return (
        <tr className="border-t border-border hover:bg-muted/30">
            <td className="px-3 py-2 font-mono text-xs">
                <Link
                    to={`/users/${m.user_id}`}
                    className="text-[var(--admin-accent-strong)] hover:underline"
                >
                    {m.email}
                </Link>
            </td>
            <td className="px-3 py-2 text-xs">
                <div>{m.owner_email}</div>
                {m.org_name && (
                    <Link
                        to={`/organizations/${m.organization_id}`}
                        className="text-[10px] text-muted-foreground hover:underline"
                    >
                        {m.org_name}
                    </Link>
                )}
            </td>
            <td className="px-3 py-2 text-xs">{m.provider}</td>
            <td className="px-3 py-2">
                {m.status === "active" ? (
                    <Badge
                        variant="outline"
                        className="text-[10px] border-emerald-300 text-emerald-700 bg-emerald-50"
                    >
                        active
                    </Badge>
                ) : (
                    <Badge
                        variant="outline"
                        className="text-[10px] border-zinc-300 text-zinc-600"
                    >
                        {m.status}
                    </Badge>
                )}
            </td>
            <td className="px-3 py-2 text-xs">
                {m.warmup_enabled ? (
                    <span className="text-emerald-600">on</span>
                ) : (
                    <span className="text-muted-foreground">off</span>
                )}
            </td>
            <td className="px-3 py-2 text-right tabular-nums">{m.campaign_limit}</td>
            <td
                className={`px-3 py-2 text-xs ${
                    never ? "text-red-600" : stale ? "text-amber-700" : "text-muted-foreground"
                }`}
            >
                {never
                    ? "never"
                    : new Date(m.last_synced_at!).toLocaleString()}
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
        { value: "inactive", label: "Inactive" },
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

function ProviderToggle({
    value,
    onChange,
}: {
    value: string;
    onChange: (v: string) => void;
}) {
    return (
        <select
            value={value}
            onChange={(e) => onChange(e.target.value)}
            className="text-xs px-2 py-1 rounded-md border border-border bg-card"
        >
            <option value="">All providers</option>
            <option value="google">Google</option>
            <option value="microsoft">Microsoft</option>
            <option value="smtp">SMTP/IMAP</option>
        </select>
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
