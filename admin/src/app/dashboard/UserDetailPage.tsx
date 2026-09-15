// User detail — composes /admin/users/:id/preview into one screen.
// Header is profile + status + action buttons; body shows orgs,
// mailboxes, recent bans, and the rate-limit override block.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import {
    ArrowLeft,
    Ban,
    CheckCircle2,
    Gauge,
    ShieldAlert,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
    getUserBans,
    getUserPreview,
} from "@/lib/api/client/admin/users";
import type { UserBan } from "@/lib/api/models/admin";
import { UserBanDialog } from "./UserBanDialog";
import { UserRateLimitsDialog } from "./UserRateLimitsDialog";

export default function UserDetailPage() {
    const { id = "" } = useParams<{ id: string }>();
    const [banDialog, setBanDialog] = useState<"ban" | "unban" | null>(null);
    const [rateLimitsOpen, setRateLimitsOpen] = useState(false);

    const previewQuery = useQuery({
        queryKey: ["admin", "users", id],
        queryFn: () => getUserPreview(id),
        enabled: !!id,
    });

    const bansQuery = useQuery({
        queryKey: ["admin", "users", id, "bans"],
        queryFn: () => getUserBans(id),
        enabled: !!id,
    });

    if (previewQuery.isLoading) return <DetailSkeleton />;
    if (previewQuery.error || !previewQuery.data) {
        return (
            <div>
                <BackLink />
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load user.
                </div>
            </div>
        );
    }

    const preview = previewQuery.data;
    const u = preview.user;
    // An instance that has not picked up the empty-slice fix still answers
    // null for these, and null.length is what took the whole page down.
    const organizations = preview.organizations ?? [];
    const mailboxes = preview.email_accounts ?? [];
    const banned = !!u.banned_at;
    const isAdmin = u.admin_permissions > 0;
    const fullName = `${u.first_name} ${u.last_name}`.trim() || u.email;
    const bans = bansQuery.data?.data ?? [];

    return (
        <div>
            <BackLink />
            <PageHeader title={fullName} description={u.email}>
                <div className="flex items-center gap-1.5">
                    {banned ? (
                        <Badge
                            variant="outline"
                            className="text-[10px] border-red-300 text-red-700 bg-red-50"
                        >
                            banned
                        </Badge>
                    ) : (
                        <Badge
                            variant="outline"
                            className="text-[10px] border-emerald-300 text-emerald-700 bg-emerald-50"
                        >
                            active
                        </Badge>
                    )}
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
                <Button
                    size="sm"
                    variant="outline"
                    onClick={() => setRateLimitsOpen(true)}
                >
                    <Gauge className="size-3.5" />
                    Rate limits
                </Button>
                {!banned ? (
                    <Button
                        size="sm"
                        onClick={() => setBanDialog("ban")}
                        disabled={isAdmin}
                        className="bg-red-600 hover:bg-red-700 text-white disabled:bg-zinc-300"
                        title={isAdmin ? "Cannot ban admin users" : undefined}
                    >
                        <Ban className="size-3.5" />
                        Ban
                    </Button>
                ) : (
                    <Button
                        size="sm"
                        onClick={() => setBanDialog("unban")}
                        className="bg-emerald-600 hover:bg-emerald-700 text-white"
                    >
                        <CheckCircle2 className="size-3.5" />
                        Unban
                    </Button>
                )}
            </PageHeader>

            <div className="grid gap-4 md:grid-cols-3 mb-6">
                <SummaryCard title="Stats">
                    <Row label="Organizations" value={u.organization_count} />
                    <Row label="Mailboxes" value={u.email_account_count} />
                    <Row label="Campaigns" value={u.campaign_count} />
                </SummaryCard>
                <SummaryCard title="Lifecycle">
                    <Row label="Joined" value={new Date(u.created_at).toLocaleDateString()} />
                    <Row label="Updated" value={new Date(u.updated_at).toLocaleDateString()} />
                    {u.banned_at && (
                        <Row
                            label="Banned"
                            value={new Date(u.banned_at).toLocaleDateString()}
                            tone="text-red-700"
                        />
                    )}
                </SummaryCard>
                <SummaryCard title="Trial">
                    <Row
                        label="Free trial used"
                        value={u.free_trial_used ? "yes" : "no"}
                    />
                    <Row label="Max orgs" value={u.max_organizations} />
                </SummaryCard>
            </div>

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">
                    Organizations
                    <span className="text-muted-foreground font-normal ml-1.5">
                        ({organizations.length})
                    </span>
                </h2>
                {organizations.length === 0 ? (
                    <Empty label="Not a member of any workspace." />
                ) : (
                    <div className="border border-border rounded-lg overflow-hidden bg-card">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                                <tr>
                                    <th className="text-left px-3 py-2 font-medium">Name</th>
                                    <th className="text-left px-3 py-2 font-medium">Slug</th>
                                    <th className="text-left px-3 py-2 font-medium">Role</th>
                                </tr>
                            </thead>
                            <tbody>
                                {organizations.map((o) => {
                                    const isOwner = o.owner_user_id === u.id;
                                    return (
                                        <tr key={o.id} className="border-t border-border hover:bg-muted/30">
                                            <td className="px-3 py-2">
                                                <Link
                                                    to={`/organizations/${o.id}`}
                                                    className="text-[var(--admin-accent-strong)] hover:underline font-medium"
                                                >
                                                    {o.name}
                                                </Link>
                                            </td>
                                            <td className="px-3 py-2 text-xs text-muted-foreground font-mono">
                                                {o.slug ?? "—"}
                                            </td>
                                            <td className="px-3 py-2 text-xs">
                                                {isOwner ? (
                                                    <span className="text-amber-700">owner</span>
                                                ) : (
                                                    <span className="text-muted-foreground">member</span>
                                                )}
                                            </td>
                                        </tr>
                                    );
                                })}
                            </tbody>
                        </table>
                    </div>
                )}
            </section>

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">
                    Mailboxes
                    <span className="text-muted-foreground font-normal ml-1.5">
                        ({mailboxes.length})
                    </span>
                </h2>
                {mailboxes.length === 0 ? (
                    <Empty label="No mailboxes connected." />
                ) : (
                    <div className="border border-border rounded-lg overflow-hidden bg-card">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                                <tr>
                                    <th className="text-left px-3 py-2 font-medium">Email</th>
                                    <th className="text-left px-3 py-2 font-medium">Provider</th>
                                    <th className="text-left px-3 py-2 font-medium">Status</th>
                                    <th className="text-left px-3 py-2 font-medium">Warmup</th>
                                    <th className="text-left px-3 py-2 font-medium">Last sync</th>
                                </tr>
                            </thead>
                            <tbody>
                                {mailboxes.map((a) => (
                                    <tr key={a.id} className="border-t border-border">
                                        <td className="px-3 py-2 font-mono text-xs">{a.email}</td>
                                        <td className="px-3 py-2 text-xs">{a.provider}</td>
                                        <td className="px-3 py-2 text-xs">{a.status}</td>
                                        <td className="px-3 py-2 text-xs">
                                            {a.warmup_enabled ? (
                                                <span className="text-emerald-600">on</span>
                                            ) : (
                                                <span className="text-muted-foreground">off</span>
                                            )}
                                        </td>
                                        <td className="px-3 py-2 text-xs text-muted-foreground">
                                            {a.last_synced_at
                                                ? new Date(a.last_synced_at).toLocaleString()
                                                : "—"}
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </section>

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">
                    Ban history
                    {bans.length > 0 && (
                        <span className="text-muted-foreground font-normal ml-1.5">
                            ({bans.length})
                        </span>
                    )}
                </h2>
                {bansQuery.isLoading ? (
                    <Skeleton className="h-24 w-full" />
                ) : bans.length === 0 ? (
                    <Empty label="No bans on record." />
                ) : (
                    <BanList bans={bans} />
                )}
            </section>

            <UserBanDialog
                userId={u.id}
                userEmail={u.email}
                mode={banDialog ?? "ban"}
                open={banDialog !== null}
                onOpenChange={(v) => !v && setBanDialog(null)}
            />
            <UserRateLimitsDialog
                userId={u.id}
                userEmail={u.email}
                current={preview.rate_limits}
                open={rateLimitsOpen}
                onOpenChange={setRateLimitsOpen}
            />
        </div>
    );
}

function BackLink() {
    return (
        <Link
            to="/users"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-2"
        >
            <ArrowLeft className="size-3" /> Back to users
        </Link>
    );
}

function SummaryCard({
    title,
    children,
}: {
    title: string;
    children: React.ReactNode;
}) {
    return (
        <div className="border border-border rounded-lg p-3 bg-card">
            <div className="text-[10px] uppercase text-muted-foreground tracking-wider mb-1">
                {title}
            </div>
            <div className="space-y-1 text-sm">{children}</div>
        </div>
    );
}

function Row({
    label,
    value,
    tone,
}: {
    label: string;
    value: React.ReactNode;
    tone?: string;
}) {
    return (
        <div className="flex justify-between items-baseline">
            <span className="text-xs text-muted-foreground">{label}</span>
            <span className={`text-sm font-medium tabular-nums ${tone ?? ""}`}>
                {value}
            </span>
        </div>
    );
}

function Empty({ label }: { label: string }) {
    return (
        <div className="text-sm text-muted-foreground border border-border rounded-md p-4 bg-card">
            {label}
        </div>
    );
}

function BanList({ bans }: { bans: UserBan[] }) {
    return (
        <div className="border border-border rounded-lg overflow-hidden bg-card">
            <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                    <tr>
                        <th className="text-left px-3 py-2 font-medium">Banned</th>
                        <th className="text-left px-3 py-2 font-medium">By</th>
                        <th className="text-left px-3 py-2 font-medium">Reason</th>
                        <th className="text-left px-3 py-2 font-medium">Status</th>
                    </tr>
                </thead>
                <tbody>
                    {bans.map((b) => (
                        <tr key={b.id} className="border-t border-border">
                            <td className="px-3 py-2 text-xs text-muted-foreground">
                                {new Date(b.banned_at).toLocaleString()}
                            </td>
                            <td className="px-3 py-2 text-xs">
                                {b.banned_by_user?.email ?? b.banned_by}
                            </td>
                            <td className="px-3 py-2 text-xs">{b.reason}</td>
                            <td className="px-3 py-2 text-xs">
                                {b.unbanned_at ? (
                                    <span className="text-emerald-600">
                                        lifted {new Date(b.unbanned_at).toLocaleDateString()}
                                    </span>
                                ) : (
                                    <span className="text-red-700">active</span>
                                )}
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
}

function DetailSkeleton() {
    return (
        <div>
            <BackLink />
            <Skeleton className="h-8 w-64 mb-2" />
            <Skeleton className="h-4 w-80 mb-6" />
            <div className="grid gap-4 md:grid-cols-3 mb-6">
                {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-24 w-full" />
                ))}
            </div>
            <Skeleton className="h-32 w-full mb-4" />
            <Skeleton className="h-32 w-full mb-4" />
            <Skeleton className="h-24 w-full" />
        </div>
    );
}
