// Organization detail — composes /admin/organizations/:id and
// /admin/organizations/:id/members into a single screen. Header summarises
// owner + plan + lifecycle; the body shows usage-vs-limits side-by-side
// and the members table.

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { ArrowLeft, Crown, Shield, SlidersHorizontal } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
    getOrganization,
    getOrganizationMembers,
} from "@/lib/api/client/admin/organizations";
import type {
    AdminOrgDetail,
    AdminOrgMember,
    OrganizationCounts,
    OrganizationLimits,
    OrganizationLimitOverrides,
} from "@/lib/api/models/admin";
import { OrganizationOverridesDialog } from "./OrganizationOverridesDialog";

export default function OrganizationDetailPage() {
    const { id = "" } = useParams<{ id: string }>();
    const [overridesOpen, setOverridesOpen] = useState(false);

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
