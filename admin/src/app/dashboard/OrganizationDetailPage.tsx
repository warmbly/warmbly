// Organization detail — composes /admin/organizations/:id and
// /admin/organizations/:id/members into a single screen. Header summarises
// owner + plan + lifecycle; the overview tab shows usage-vs-limits and the
// members table, with API keys, webhooks and transfers on their own tabs
// (?tab= so links deep-link).

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import {
    ArrowLeft,
    ArrowLeftRight,
    Ban,
    Crown,
    KeyRound,
    LayoutDashboard,
    Shield,
    SlidersHorizontal,
    Webhook,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { PageTabs } from "@/components/layout/PageTabs";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import { ErrorState } from "@/components/ErrorState";
import {
    getOrganization,
    getOrganizationMembers,
    listOrganizationAPIKeys,
    listOrganizationWebhooks,
    revokeOrganizationAPIKey,
    type AdminOrgAPIKey,
    type AdminWebhookEndpointRow,
} from "@/lib/api/client/admin/organizations";
import { OrgTransferTab } from "./transfers/OrgTransferTab";
import { fmtAgo, fmtDate, fmtDateTime } from "./fleet/format";
import type {
    AdminOrgDetail,
    AdminOrgMember,
    OrganizationCounts,
    OrganizationLimits,
    OrganizationLimitOverrides,
} from "@/lib/api/models/admin";
import { OrganizationOverridesDialog } from "./OrganizationOverridesDialog";
import { OrganizationRiskCard, RiskBadge } from "./OrganizationRiskCard";
import { OrganizationManagedPlanCard } from "./OrganizationManagedPlanCard";

const TABS = [
    { id: "overview", label: "Overview", icon: LayoutDashboard },
    { id: "api-keys", label: "API keys", icon: KeyRound },
    { id: "webhooks", label: "Webhooks", icon: Webhook },
    { id: "transfer", label: "Transfer", icon: ArrowLeftRight },
] as const;

type TabId = (typeof TABS)[number]["id"];

function isTab(v: string | null): v is TabId {
    return TABS.some((t) => t.id === v);
}

export default function OrganizationDetailPage() {
    const { id = "" } = useParams<{ id: string }>();
    const [overridesOpen, setOverridesOpen] = useState(false);
    const [params, setParams] = useSearchParams();
    const rawTab = params.get("tab");
    const tab: TabId = isTab(rawTab) ? rawTab : "overview";

    function setTab(next: string) {
        setParams(
            (p) => {
                p.set("tab", next);
                return p;
            },
            { replace: true },
        );
    }

    const orgQuery = useQuery({
        queryKey: ["admin", "organizations", id],
        queryFn: () => getOrganization(id),
        enabled: !!id,
    });

    const membersQuery = useQuery({
        queryKey: ["admin", "organizations", id, "members"],
        queryFn: () => getOrganizationMembers(id),
        enabled: !!id,
    });

    if (orgQuery.isLoading) return <DetailSkeleton />;
    if (orgQuery.error || !orgQuery.data) {
        return (
            <div>
                <BackLink />
                <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                    Failed to load organization.
                </div>
            </div>
        );
    }

    const org = orgQuery.data;
    return (
        <div>
            <BackLink />
            <PageHeader
                title={org.name}
                description={org.slug ?? "No slug set"}
            >
                <StatusPills org={org} />
            </PageHeader>

            <PageTabs tabs={[...TABS]} value={tab} onChange={setTab} />

            {tab === "api-keys" && <APIKeysTab orgId={org.id} />}
            {tab === "webhooks" && <WebhooksTab orgId={org.id} />}
            {tab === "transfer" && <OrgTransferTab orgId={org.id} orgName={org.name} />}

            {tab === "overview" && (
            <>
            <div className="grid gap-4 md:grid-cols-3">
                <SummaryCard title="Owner">
                    <div className="text-sm font-medium">
                        {`${org.owner_first_name} ${org.owner_last_name}`.trim() ||
                            org.owner_email}
                    </div>
                    <div className="text-xs text-muted-foreground break-all">
                        {org.owner_email}
                    </div>
                    {org.owner_banned_at && (
                        <Badge
                            variant="outline"
                            className="mt-2 text-[10px] border-red-300 text-red-700 bg-red-50"
                        >
                            owner banned {new Date(org.owner_banned_at).toLocaleDateString()}
                        </Badge>
                    )}
                </SummaryCard>

                <SummaryCard title="Plan">
                    <div className="text-sm font-medium">
                        {org.plan_name ?? (
                            <span className="text-muted-foreground">No active plan</span>
                        )}
                    </div>
                    <div className="text-xs text-muted-foreground">
                        {org.subscription_status ?? "—"}
                        {org.is_enterprise && (
                            <Badge
                                variant="outline"
                                className="ml-1.5 text-[10px] border-purple-300 text-purple-700 bg-purple-50"
                            >
                                enterprise
                            </Badge>
                        )}
                    </div>
                    {org.current_period_end && (
                        <div className="text-[10px] text-muted-foreground mt-1">
                            renews {new Date(org.current_period_end).toLocaleDateString()}
                        </div>
                    )}
                    {org.trial_end && !org.current_period_end && (
                        <div className="text-[10px] text-muted-foreground mt-1">
                            trial ends {new Date(org.trial_end).toLocaleDateString()}
                        </div>
                    )}
                </SummaryCard>

                <SummaryCard title="Lifecycle">
                    <div className="text-xs text-muted-foreground">
                        Created {new Date(org.created_at).toLocaleDateString()}
                    </div>
                    <div className="text-xs text-muted-foreground">
                        Updated {new Date(org.updated_at).toLocaleDateString()}
                    </div>
                    {org.deletion_scheduled_for && (
                        <div className="text-xs text-amber-700 mt-2">
                            Deletion scheduled for{" "}
                            {new Date(org.deletion_scheduled_for).toLocaleDateString()}
                        </div>
                    )}
                </SummaryCard>
            </div>

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">Plan</h2>
                <OrganizationManagedPlanCard orgId={org.id} />
            </section>

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">Abuse posture</h2>
                <OrganizationRiskCard orgId={org.id} />
            </section>

            <section className="mt-6">
                <div className="flex items-center justify-between mb-2">
                    <h2 className="text-sm font-semibold">Usage vs. effective limits</h2>
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setOverridesOpen(true)}
                    >
                        <SlidersHorizontal className="size-3.5" />
                        Edit overrides
                    </Button>
                </div>
                <UsageTable
                    counts={org.counts ?? null}
                    planLimits={org.limits ?? null}
                    effectiveLimits={org.effective_limits ?? null}
                    overrides={org.overrides ?? null}
                />
                {org.overrides && (
                    <p className="text-[10px] text-muted-foreground mt-2">
                        Overrides last set{" "}
                        {new Date(org.overrides.updated_at).toLocaleString()}
                        {org.overrides.notes && (
                            <> · "{org.overrides.notes}"</>
                        )}
                    </p>
                )}
            </section>

            <OrganizationOverridesDialog
                org={org}
                open={overridesOpen}
                onOpenChange={setOverridesOpen}
            />

            <section className="mt-6">
                <h2 className="text-sm font-semibold mb-2">
                    Members
                    {membersQuery.data && (
                        <span className="text-muted-foreground font-normal ml-1.5">
                            ({membersQuery.data.data.length})
                        </span>
                    )}
                </h2>
                {membersQuery.isLoading ? (
                    <Skeleton className="h-32 w-full" />
                ) : membersQuery.error ? (
                    <div className="text-sm text-red-600 border border-red-200 bg-red-50 rounded-md p-3">
                        Failed to load members.
                    </div>
                ) : (
                    <MembersTable members={membersQuery.data?.data ?? []} />
                )}
            </section>
            </>
            )}
        </div>
    );
}

function BackLink() {
    return (
        <Link
            to="/organizations"
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-2"
        >
            <ArrowLeft className="size-3" /> Back to organizations
        </Link>
    );
}

function StatusPills({ org }: { org: AdminOrgDetail }) {
    return (
        <div className="flex items-center gap-1.5">
            {org.deletion_scheduled_for ? (
                <Badge
                    variant="outline"
                    className="text-[10px] border-amber-300 text-amber-700 bg-amber-50"
                >
                    pending deletion
                </Badge>
            ) : (
                <Badge
                    variant="outline"
                    className="text-[10px] border-emerald-300 text-emerald-700 bg-emerald-50"
                >
                    active
                </Badge>
            )}
            {org.is_enterprise && (
                <Badge
                    variant="outline"
                    className="text-[10px] border-purple-300 text-purple-700 bg-purple-50"
                >
                    enterprise
                </Badge>
            )}
            {org.risk_state && org.risk_state !== "trusted" && (
                <RiskBadge state={org.risk_state} />
            )}
        </div>
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
            {children}
        </div>
    );
}

type LimitField =
    | "max_active_campaigns"
    | "max_campaigns"
    | "max_email_accounts"
    | "max_team_members"
    | "max_contacts"
    | "daily_campaign_limit";

type UsageRow = {
    label: string;
    field: LimitField;
    current: number;
    countField: keyof OrganizationCounts;
};

const USAGE_ROWS: UsageRow[] = [
    { label: "Active campaigns", field: "max_active_campaigns", current: 0, countField: "active_campaigns" },
    { label: "Total campaigns", field: "max_campaigns", current: 0, countField: "total_campaigns" },
    { label: "Email accounts (mailboxes)", field: "max_email_accounts", current: 0, countField: "email_accounts" },
    { label: "Team members", field: "max_team_members", current: 0, countField: "total_members" },
    { label: "Contacts", field: "max_contacts", current: 0, countField: "total_contacts" },
    { label: "Emails sent today", field: "daily_campaign_limit", current: 0, countField: "emails_sent_today" },
];

function UsageTable({
    counts,
    planLimits,
    effectiveLimits,
    overrides,
}: {
    counts: OrganizationCounts | null;
    planLimits: OrganizationLimits | null;
    effectiveLimits: OrganizationLimits | null;
    overrides: OrganizationLimitOverrides | null;
}) {
    return (
        <div className="border border-border rounded-lg overflow-hidden bg-card">
            <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                    <tr>
                        <th className="text-left px-3 py-2 font-medium">Resource</th>
                        <th className="text-right px-3 py-2 font-medium">Used</th>
                        <th className="text-right px-3 py-2 font-medium">Plan</th>
                        <th className="text-right px-3 py-2 font-medium">Override</th>
                        <th className="text-right px-3 py-2 font-medium">Effective</th>
                        <th className="text-left px-3 py-2 font-medium">Headroom</th>
                    </tr>
                </thead>
                <tbody>
                    {USAGE_ROWS.map((r) => {
                        const current = counts?.[r.countField] ?? 0;
                        const plan = planLimits?.[r.field] ?? null;
                        const override = overrides?.[r.field as keyof OrganizationLimitOverrides] as number | undefined;
                        const overrideActive = !!override && override > 0;
                        const effective = effectiveLimits?.[r.field] ?? null;

                        const pct =
                            effective != null && effective > 0
                                ? Math.min(100, (current / effective) * 100)
                                : 0;
                        const over = effective != null && current > effective;

                        return (
                            <tr key={r.label} className="border-t border-border">
                                <td className="px-3 py-2">{r.label}</td>
                                <td className="px-3 py-2 text-right tabular-nums">
                                    {current.toLocaleString()}
                                </td>
                                <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                                    {plan != null ? plan.toLocaleString() : "—"}
                                </td>
                                <td
                                    className={`px-3 py-2 text-right tabular-nums ${
                                        overrideActive
                                            ? "text-[var(--admin-accent-strong)] font-medium"
                                            : "text-muted-foreground"
                                    }`}
                                >
                                    {overrideActive ? override!.toLocaleString() : "—"}
                                </td>
                                <td className="px-3 py-2 text-right tabular-nums font-medium">
                                    {effective != null ? effective.toLocaleString() : "—"}
                                </td>
                                <td className="px-3 py-2 w-48">
                                    {effective == null ? (
                                        <span className="text-xs text-muted-foreground">
                                            unbounded
                                        </span>
                                    ) : (
                                        <div className="flex items-center gap-2">
                                            <div className="h-1.5 flex-1 bg-muted rounded overflow-hidden">
                                                <div
                                                    className={`h-full ${
                                                        over
                                                            ? "bg-red-500"
                                                            : pct > 80
                                                            ? "bg-amber-500"
                                                            : "bg-emerald-500"
                                                    }`}
                                                    style={{ width: `${pct}%` }}
                                                />
                                            </div>
                                            <span
                                                className={`text-[10px] tabular-nums ${
                                                    over ? "text-red-600" : "text-muted-foreground"
                                                }`}
                                            >
                                                {Math.round(pct)}%
                                            </span>
                                        </div>
                                    )}
                                </td>
                            </tr>
                        );
                    })}
                </tbody>
            </table>
        </div>
    );
}

function MembersTable({ members }: { members: AdminOrgMember[] }) {
    if (members.length === 0) {
        return (
            <div className="text-sm text-muted-foreground border border-border rounded-md p-4 bg-card">
                No members.
            </div>
        );
    }
    return (
        <div className="border border-border rounded-lg overflow-hidden bg-card">
            <table className="w-full text-sm">
                <thead className="bg-muted/50 text-muted-foreground text-xs uppercase">
                    <tr>
                        <th className="text-left px-3 py-2 font-medium">Member</th>
                        <th className="text-left px-3 py-2 font-medium">Role</th>
                        <th className="text-left px-3 py-2 font-medium">Joined</th>
                    </tr>
                </thead>
                <tbody>
                    {members.map((m) => {
                        const name =
                            `${m.user?.first_name ?? ""} ${m.user?.last_name ?? ""}`.trim() ||
                            m.user?.email ||
                            m.user_id;
                        const isOwner = m.role === "owner";
                        return (
                            <tr key={m.id} className="border-t border-border hover:bg-muted/30">
                                <td className="px-3 py-2">
                                    <div className="font-medium">{name}</div>
                                    {m.user?.email && (
                                        <div className="text-[10px] text-muted-foreground">
                                            {m.user.email}
                                        </div>
                                    )}
                                </td>
                                <td className="px-3 py-2">
                                    <div className="inline-flex items-center gap-1 text-xs">
                                        {isOwner ? (
                                            <Crown className="size-3 text-amber-600" />
                                        ) : (
                                            <Shield className="size-3 text-muted-foreground" />
                                        )}
                                        {m.role}
                                    </div>
                                </td>
                                <td className="px-3 py-2 text-xs text-muted-foreground">
                                    {m.accepted_at
                                        ? new Date(m.accepted_at).toLocaleDateString()
                                        : `invited ${new Date(m.invited_at).toLocaleDateString()}`}
                                </td>
                            </tr>
                        );
                    })}
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
            <Skeleton className="h-4 w-96 mb-6" />
            <div className="grid gap-4 md:grid-cols-3 mb-6">
                {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-24 w-full" />
                ))}
            </div>
            <Skeleton className="h-48 w-full mb-4" />
            <Skeleton className="h-40 w-full" />
        </div>
    );
}

// ---- API keys ----

const KEY_STATUS_TONE: Record<string, string> = {
    active: "border-emerald-300 bg-emerald-50 text-emerald-700",
    revoked: "border-red-300 bg-red-50 text-red-700",
    expired: "border-zinc-300 bg-zinc-50 text-zinc-500",
};

function APIKeysTab({ orgId }: { orgId: string }) {
    const qc = useQueryClient();
    const [revoking, setRevoking] = useState<AdminOrgAPIKey | null>(null);
    const [reason, setReason] = useState("");

    const keysQ = useQuery({
        queryKey: ["admin", "organizations", orgId, "api-keys"],
        queryFn: () => listOrganizationAPIKeys(orgId),
    });

    const revoke = useMutation({
        mutationFn: (k: AdminOrgAPIKey) => revokeOrganizationAPIKey(orgId, k.id, reason.trim() || undefined),
        onSuccess: () => {
            toast.success("API key revoked");
            qc.invalidateQueries({ queryKey: ["admin", "organizations", orgId, "api-keys"] });
            setRevoking(null);
            setReason("");
        },
        onError: (e: Error) => toast.error(e.message || "Revoke failed"),
    });

    const keys = keysQ.data?.data ?? [];

    return (
        <section>
            <p className="mb-3 max-w-2xl text-[12.5px] text-muted-foreground">
                Keys the workspace minted for the public API. The secret is never shown; revoking is immediate and is
                recorded in the admin audit log with the reason.
            </p>
            {keysQ.isLoading ? (
                <Skeleton className="h-32 w-full" />
            ) : keysQ.error ? (
                <ErrorState error={keysQ.error} title="Failed to load API keys" onRetry={() => keysQ.refetch()} />
            ) : keys.length === 0 ? (
                <div className="rounded-md border border-border bg-card p-4 text-sm text-muted-foreground">
                    This workspace has not created any API keys.
                </div>
            ) : (
                <div className="overflow-hidden rounded-lg border border-border bg-card">
                    <div className="overflow-x-auto">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/40 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
                                <tr>
                                    <th className="px-3 py-2 text-left">Name</th>
                                    <th className="px-3 py-2 text-left">Key</th>
                                    <th className="px-3 py-2 text-left">Status</th>
                                    <th className="px-3 py-2 text-left">User</th>
                                    <th className="px-3 py-2 text-left">Last used</th>
                                    <th className="px-3 py-2 text-right">Requests 7d</th>
                                    <th className="px-3 py-2 text-left">Expires</th>
                                    <th className="px-3 py-2 text-left">Created</th>
                                    <th className="px-3 py-2 text-right" />
                                </tr>
                            </thead>
                            <tbody>
                                {keys.map((k) => (
                                    <tr key={k.id} className="border-t border-border">
                                        <td className="px-3 py-2 font-medium">{k.name || <span className="text-muted-foreground">untitled</span>}</td>
                                        <td className="px-3 py-2 font-mono text-[11px]">
                                            {k.key_prefix}…{k.key_suffix}
                                        </td>
                                        <td className="px-3 py-2">
                                            <Badge variant="outline" className={`text-[10px] ${KEY_STATUS_TONE[k.status] ?? "border-zinc-300 text-zinc-600"}`}>
                                                {k.status}
                                            </Badge>
                                        </td>
                                        <td className="px-3 py-2 text-xs">{k.user_email || k.user_id.slice(0, 8)}</td>
                                        <td className="px-3 py-2 text-xs text-muted-foreground" title={fmtDateTime(k.last_used_at)}>
                                            {k.last_used_at ? fmtAgo(k.last_used_at) : "never"}
                                        </td>
                                        <td className="px-3 py-2 text-right text-xs tabular-nums">{k.requests_last_7d.toLocaleString()}</td>
                                        <td className="px-3 py-2 text-xs text-muted-foreground">{k.expires_at ? fmtDate(k.expires_at) : "never"}</td>
                                        <td className="px-3 py-2 text-xs text-muted-foreground">{fmtDate(k.created_at)}</td>
                                        <td className="px-3 py-2 text-right">
                                            {k.status === "active" && (
                                                <Button
                                                    size="xs"
                                                    variant="outline"
                                                    className="text-red-700 hover:bg-red-50"
                                                    onClick={() => {
                                                        setReason("");
                                                        setRevoking(k);
                                                    }}
                                                >
                                                    <Ban className="size-3" />
                                                    Revoke
                                                </Button>
                                            )}
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}

            <Dialog
                open={!!revoking}
                onOpenChange={(v) => {
                    if (!v && !revoke.isPending) setRevoking(null);
                }}
            >
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>Revoke this API key?</DialogTitle>
                        <DialogDescription>
                            {revoking?.name ? `"${revoking.name}"` : "This key"} ({revoking?.key_prefix}…{revoking?.key_suffix}) stops
                            authenticating immediately. Anything the workspace built on it fails on its next request.
                        </DialogDescription>
                    </DialogHeader>
                    <div className="space-y-1.5">
                        <Label htmlFor="revoke-reason" className="text-xs">
                            Reason <span className="font-normal text-muted-foreground">(optional, goes to the audit log)</span>
                        </Label>
                        <Input
                            id="revoke-reason"
                            value={reason}
                            onChange={(e) => setReason(e.target.value)}
                            placeholder="e.g. leaked in a public repository"
                            className="h-8 text-[12.5px]"
                            autoFocus
                        />
                    </div>
                    <DialogFooter>
                        <Button variant="outline" onClick={() => setRevoking(null)} disabled={revoke.isPending}>
                            Cancel
                        </Button>
                        <Button variant="destructive" onClick={() => revoking && revoke.mutate(revoking)} disabled={revoke.isPending}>
                            {revoke.isPending ? "Revoking…" : "Revoke key"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </section>
    );
}

// ---- webhooks ----

function WebhooksTab({ orgId }: { orgId: string }) {
    const hooksQ = useQuery({
        queryKey: ["admin", "organizations", orgId, "webhooks"],
        queryFn: () => listOrganizationWebhooks(orgId),
    });
    const hooks = hooksQ.data?.data ?? [];

    return (
        <section>
            <p className="mb-3 max-w-2xl text-[12.5px] text-muted-foreground">
                Endpoints the workspace registered for event delivery. Consecutive failures and the last failure reason
                are what the delivery loop sees; drops are events skipped because the endpoint was disabled or over its
                failure ceiling.
            </p>
            {hooksQ.isLoading ? (
                <Skeleton className="h-32 w-full" />
            ) : hooksQ.error ? (
                <ErrorState error={hooksQ.error} title="Failed to load webhooks" onRetry={() => hooksQ.refetch()} />
            ) : hooks.length === 0 ? (
                <div className="rounded-md border border-border bg-card p-4 text-sm text-muted-foreground">
                    This workspace has no webhook endpoints.
                </div>
            ) : (
                <div className="overflow-hidden rounded-lg border border-border bg-card">
                    <div className="overflow-x-auto">
                        <table className="w-full text-sm">
                            <thead className="bg-muted/40 text-[10.5px] font-semibold uppercase tracking-wider text-muted-foreground">
                                <tr>
                                    <th className="px-3 py-2 text-left">Endpoint</th>
                                    <th className="px-3 py-2 text-left">Enabled</th>
                                    <th className="px-3 py-2 text-left">Events</th>
                                    <th className="px-3 py-2 text-right">Failures in a row</th>
                                    <th className="px-3 py-2 text-left">Last success</th>
                                    <th className="px-3 py-2 text-left">Last failure</th>
                                    <th className="px-3 py-2 text-right">7d delivered / failed / drops</th>
                                </tr>
                            </thead>
                            <tbody>
                                {hooks.map((h: AdminWebhookEndpointRow) => (
                                    <tr key={h.id} className="border-t border-border align-top">
                                        <td className="px-3 py-2">
                                            <div className="max-w-xs truncate font-mono text-[11px]" title={h.url}>
                                                {h.url}
                                            </div>
                                            {h.description && <div className="text-[11px] text-muted-foreground">{h.description}</div>}
                                        </td>
                                        <td className="px-3 py-2">
                                            <Badge
                                                variant="outline"
                                                className={`text-[10px] ${h.enabled ? "border-emerald-300 bg-emerald-50 text-emerald-700" : "border-zinc-300 text-zinc-500"}`}
                                            >
                                                {h.enabled ? "enabled" : "disabled"}
                                            </Badge>
                                        </td>
                                        <td className="px-3 py-2">
                                            <div className="flex max-w-xs flex-wrap gap-1">
                                                {(h.event_types ?? []).length === 0 ? (
                                                    <span className="text-xs text-muted-foreground">all</span>
                                                ) : (
                                                    (h.event_types ?? []).map((t) => (
                                                        <Badge key={t} variant="outline" className="font-mono text-[9px]">
                                                            {t}
                                                        </Badge>
                                                    ))
                                                )}
                                            </div>
                                        </td>
                                        <td className={`px-3 py-2 text-right text-xs tabular-nums ${h.consecutive_failures > 0 ? "font-medium text-red-600" : "text-muted-foreground"}`}>
                                            {h.consecutive_failures}
                                        </td>
                                        <td className="px-3 py-2 text-xs text-muted-foreground" title={fmtDateTime(h.last_success_at)}>
                                            {h.last_success_at ? fmtAgo(h.last_success_at) : "never"}
                                        </td>
                                        <td className="px-3 py-2 text-xs">
                                            <div className="text-muted-foreground" title={fmtDateTime(h.last_failure_at)}>
                                                {h.last_failure_at ? fmtAgo(h.last_failure_at) : "never"}
                                            </div>
                                            {h.last_failure_reason && (
                                                <div className="max-w-xs truncate text-[11px] text-red-600" title={h.last_failure_reason}>
                                                    {h.last_failure_reason}
                                                </div>
                                            )}
                                        </td>
                                        <td className="px-3 py-2 text-right text-xs tabular-nums">
                                            {h.deliveries_last_7d.toLocaleString()}
                                            <span className="text-muted-foreground"> / </span>
                                            <span className={h.failed_last_7d > 0 ? "text-red-600" : ""}>{h.failed_last_7d.toLocaleString()}</span>
                                            <span className="text-muted-foreground"> / </span>
                                            <span className={h.drops_last_7d > 0 ? "text-amber-700" : ""}>{h.drops_last_7d.toLocaleString()}</span>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}
        </section>
    );
}
