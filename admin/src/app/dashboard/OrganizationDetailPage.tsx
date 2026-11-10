// Organization detail: composes /admin/organizations/:id and
// /admin/organizations/:id/members into a single screen. The overview is a
// main column plus a properties column: usage, members, plan and posture on the left,
// owner, plan and lifecycle as properties on the right. API keys, webhooks and
// transfers sit on their own tabs (?tab= so links deep-link).

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { toast } from "sonner";
import {
    ArrowLeftRight,
    Ban,
    Crown,
    KeyRound,
    LayoutDashboard,
    Shield,
    SlidersHorizontal,
    Users,
    Webhook,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { PageTabs } from "@/components/layout/PageTabs";
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
import { EmptyState, Property, PropertyList, Section, StatusBadge } from "@/components/ui/kit";
import { ErrorState } from "@/components/ErrorState";
import { TONE_DOT, TONE_TEXT, type Tone } from "@/lib/tones";
import { cn } from "@/lib/utils";
import {
    getOrganization,
    getOrganizationMembers,
    listOrganizationAPIKeys,
    listOrganizationWebhooks,
    revokeOrganizationAPIKey,
    type AdminOrgAPIKey,
    type AdminWebhookEndpointRow,
} from "@/lib/api/client/admin/organizations";
import { getOrganizationAgency } from "@/lib/api/client/admin/agencies";
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
import { OrganizationPlanOffersCard } from "./OrganizationPlanOffersCard";

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

const CRUMBS = [{ label: "Organizations", to: "/organizations" }];

// Shared hand-rolled table styling, matching DataTable.
const TABLE = "w-full border-collapse text-[13px]";
const TH = "h-9 whitespace-nowrap px-3 text-left text-xs font-medium text-muted-foreground first:pl-4 last:pr-4";
const TR = "border-b border-border/70 transition-colors last:border-0 hover:bg-accent/50";
const TD = "h-10 px-3 py-2 first:pl-4 last:pr-4";

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
                <PageHeader breadcrumbs={CRUMBS} title="Organization" />
                <ErrorState
                    error={orgQuery.error}
                    title="Failed to load organization."
                    onRetry={() => orgQuery.refetch()}
                />
            </div>
        );
    }

    const org = orgQuery.data;
    return (
        <div>
            <PageHeader breadcrumbs={CRUMBS} title={org.name} meta={<StatusPills org={org} />} />

            <PageTabs tabs={[...TABS]} value={tab} onChange={setTab} />

            {tab === "api-keys" && <APIKeysTab orgId={org.id} />}
            {tab === "webhooks" && <WebhooksTab orgId={org.id} />}
            {tab === "transfer" && <OrgTransferTab orgId={org.id} orgName={org.name} />}

            {tab === "overview" && (
                <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_17rem] lg:gap-10">
                    <div className="min-w-0 lg:order-1 order-2">
                        <Section
                            title="Usage vs. effective limits"
                            actions={
                                <Button size="sm" variant="outline" onClick={() => setOverridesOpen(true)}>
                                    <SlidersHorizontal className="size-3.5" />
                                    Edit overrides
                                </Button>
                            }
                        >
                            <UsageTable
                                counts={org.counts ?? null}
                                planLimits={org.limits ?? null}
                                effectiveLimits={org.effective_limits ?? null}
                                overrides={org.overrides ?? null}
                            />
                            {org.overrides && (
                                <p className="mt-2 text-xs text-muted-foreground">
                                    Overrides last set{" "}
                                    {new Date(org.overrides.updated_at).toLocaleString()}
                                    {org.overrides.notes && (
                                        <> · "{org.overrides.notes}"</>
                                    )}
                                </p>
                            )}
                        </Section>

                        <OrganizationOverridesDialog
                            org={org}
                            open={overridesOpen}
                            onOpenChange={setOverridesOpen}
                        />

                        <Section
                            title={
                                <span className="inline-flex items-center gap-1.5">
                                    Members
                                    {membersQuery.data && (
                                        <span className="font-normal tabular-nums text-muted-foreground">
                                            {membersQuery.data.data.length}
                                        </span>
                                    )}
                                </span>
                            }
                        >
                            {membersQuery.isLoading ? (
                                <Skeleton className="h-32 w-full rounded-lg" />
                            ) : membersQuery.error ? (
                                <ErrorState
                                    error={membersQuery.error}
                                    title="Failed to load members."
                                    onRetry={() => membersQuery.refetch()}
                                />
                            ) : (
                                <MembersTable members={membersQuery.data?.data ?? []} />
                            )}
                        </Section>

                        <Section title="Plan">
                            <div className="space-y-3">
                                <OrganizationManagedPlanCard orgId={org.id} />
                                <OrganizationPlanOffersCard orgId={org.id} />
                            </div>
                        </Section>

                        <Section title="Abuse posture">
                            <OrganizationRiskCard orgId={org.id} />
                        </Section>
                    </div>

                    <aside className="order-1 min-w-0 lg:order-2">
                        <OrgProperties org={org} />
                    </aside>
                </div>
            )}
        </div>
    );
}

const subtle = "text-subtle-foreground";

function OrgProperties({ org }: { org: AdminOrgDetail }) {
    const ownerName = `${org.owner_first_name} ${org.owner_last_name}`.trim() || org.owner_email;
    return (
        <div className="lg:sticky lg:top-16">
            <div className="mb-1 text-xs font-medium text-muted-foreground">Properties</div>
            <PropertyList className="border-b border-border">
                <Property label="Status">
                    {org.deletion_scheduled_for ? (
                        <StatusBadge tone="warning" dot>
                            pending deletion
                        </StatusBadge>
                    ) : (
                        <StatusBadge tone="success" dot>
                            active
                        </StatusBadge>
                    )}
                </Property>
                <Property label="Slug">
                    {org.slug ? (
                        <span className="font-mono text-[12px]">{org.slug}</span>
                    ) : (
                        <span className={subtle}>No slug set</span>
                    )}
                </Property>
                <Property label="Owner">
                    <div className="font-medium">{ownerName}</div>
                    <div className="break-all text-xs text-muted-foreground">{org.owner_email}</div>
                    {org.owner_banned_at && (
                        <StatusBadge tone="danger" className="mt-1.5">
                            owner banned {new Date(org.owner_banned_at).toLocaleDateString()}
                        </StatusBadge>
                    )}
                </Property>
            </PropertyList>

            <div className="mt-5 mb-1 text-xs font-medium text-muted-foreground">Plan</div>
            <PropertyList className="border-b border-border">
                <Property label="Plan">
                    {org.plan_name ?? <span className={subtle}>No active plan</span>}
                </Property>
                <Property label="Subscription">
                    <span className="inline-flex flex-wrap items-center gap-1.5">
                        <span className={org.subscription_status ? undefined : subtle}>
                            {org.subscription_status ?? "—"}
                        </span>
                        {org.is_enterprise && <StatusBadge tone="accent">enterprise</StatusBadge>}
                    </span>
                </Property>
                {org.current_period_end && (
                    <Property label="Renews">
                        <span className="tabular-nums">{new Date(org.current_period_end).toLocaleDateString()}</span>
                    </Property>
                )}
                {org.trial_end && !org.current_period_end && (
                    <Property label="Trial ends">
                        <span className="tabular-nums">{new Date(org.trial_end).toLocaleDateString()}</span>
                    </Property>
                )}
                <AgencyProperty orgId={org.id} />
            </PropertyList>

            <div className="mt-5 mb-1 text-xs font-medium text-muted-foreground">Lifecycle</div>
            <PropertyList>
                <Property label="Created">
                    <span className="tabular-nums">{new Date(org.created_at).toLocaleDateString()}</span>
                </Property>
                <Property label="Updated">
                    <span className="tabular-nums">{new Date(org.updated_at).toLocaleDateString()}</span>
                </Property>
                {org.deletion_scheduled_for && (
                    <Property label="Deletion">
                        <span className={cn("tabular-nums", TONE_TEXT.warning)}>
                            Scheduled for {new Date(org.deletion_scheduled_for).toLocaleDateString()}
                        </span>
                    </Property>
                )}
            </PropertyList>
        </div>
    );
}

// The agency this workspace is a client of; nothing when it stands alone.
function AgencyProperty({ orgId }: { orgId: string }) {
    const agencyQuery = useQuery({
        queryKey: ["admin", "organizations", orgId, "agency"],
        queryFn: () => getOrganizationAgency(orgId),
    });
    const agency = agencyQuery.data?.agency;
    if (!agency) return null;
    return (
        <Property label="Agency">
            <Link to={`/agencies/${agency.id}`} className="underline underline-offset-2">
                Client of {agency.name}
            </Link>
        </Property>
    );
}

function StatusPills({ org }: { org: AdminOrgDetail }) {
    return (
        <div className="flex items-center gap-1.5">
            {org.deletion_scheduled_for ? (
                <StatusBadge tone="warning" dot>
                    pending deletion
                </StatusBadge>
            ) : (
                <StatusBadge tone="success" dot>
                    active
                </StatusBadge>
            )}
            {org.is_enterprise && <StatusBadge tone="accent">enterprise</StatusBadge>}
            {org.risk_state && org.risk_state !== "trusted" && (
                <RiskBadge state={org.risk_state} />
            )}
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
        <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
            <div className="overflow-x-auto">
                <table className={TABLE}>
                    <thead>
                        <tr className="border-b border-border">
                            <th className={TH}>Resource</th>
                            <th className={cn(TH, "text-right")}>Used</th>
                            <th className={cn(TH, "text-right")}>Plan</th>
                            <th className={cn(TH, "text-right")}>Override</th>
                            <th className={cn(TH, "text-right")}>Effective</th>
                            <th className={TH}>Headroom</th>
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
                            const tone: Tone = over ? "danger" : pct > 80 ? "warning" : "success";

                            return (
                                <tr key={r.label} className={TR}>
                                    <td className={cn(TD, "whitespace-nowrap text-foreground")}>{r.label}</td>
                                    <td className={cn(TD, "text-right tabular-nums")}>
                                        {current.toLocaleString()}
                                    </td>
                                    <td className={cn(TD, "text-right tabular-nums text-muted-foreground")}>
                                        {plan != null ? plan.toLocaleString() : "—"}
                                    </td>
                                    <td
                                        className={cn(
                                            TD,
                                            "text-right tabular-nums",
                                            overrideActive
                                                ? "font-medium text-[var(--admin-accent-strong)]"
                                                : "text-muted-foreground",
                                        )}
                                    >
                                        {overrideActive ? override!.toLocaleString() : "—"}
                                    </td>
                                    <td className={cn(TD, "text-right font-medium tabular-nums")}>
                                        {effective != null ? effective.toLocaleString() : "—"}
                                    </td>
                                    <td className={cn(TD, "w-44 min-w-36")}>
                                        {effective == null ? (
                                            <span className="text-xs text-subtle-foreground">unbounded</span>
                                        ) : (
                                            <div className="flex items-center gap-2">
                                                <div className="h-1 flex-1 overflow-hidden rounded-full bg-muted">
                                                    <div
                                                        className={cn("h-full rounded-full", TONE_DOT[tone])}
                                                        style={{ width: `${pct}%` }}
                                                    />
                                                </div>
                                                <span
                                                    className={cn(
                                                        "w-9 text-right text-xs tabular-nums",
                                                        over ? TONE_TEXT.danger : "text-muted-foreground",
                                                    )}
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
        </div>
    );
}

function initials(name: string) {
    const parts = name.split(/[\s@.]+/).filter(Boolean);
    return ((parts[0]?.[0] ?? "") + (parts[1]?.[0] ?? "")).toUpperCase() || "?";
}

function MembersTable({ members }: { members: AdminOrgMember[] }) {
    if (members.length === 0) {
        return (
            <div className="surface-lit rounded-xl border border-border bg-card">
                <EmptyState icon={Users} title="No members." className="py-10" />
            </div>
        );
    }
    return (
        <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
            <div className="overflow-x-auto">
                <table className={TABLE}>
                    <thead>
                        <tr className="border-b border-border">
                            <th className={TH}>Member</th>
                            <th className={TH}>Role</th>
                            <th className={TH}>Joined</th>
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
                                <tr key={m.id} className={TR}>
                                    <td className={TD}>
                                        <div className="flex items-center gap-2.5">
                                            <span className="grid size-6 shrink-0 place-items-center rounded-full bg-muted text-[10.5px] font-medium text-muted-foreground">
                                                {initials(name)}
                                            </span>
                                            <div className="min-w-0 leading-tight">
                                                <div className="truncate font-medium text-foreground">{name}</div>
                                                {m.user?.email && (
                                                    <div className="mt-0.5 truncate text-xs text-muted-foreground">
                                                        {m.user.email}
                                                    </div>
                                                )}
                                            </div>
                                        </div>
                                    </td>
                                    <td className={TD}>
                                        <span className="inline-flex items-center gap-1.5 text-[13px]">
                                            {isOwner ? (
                                                <Crown className={cn("size-3.5", TONE_TEXT.warning)} />
                                            ) : (
                                                <Shield className="size-3.5 text-subtle-foreground" />
                                            )}
                                            {m.role}
                                        </span>
                                    </td>
                                    <td className={cn(TD, "whitespace-nowrap tabular-nums text-muted-foreground")}>
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
        </div>
    );
}

function DetailSkeleton() {
    return (
        <div>
            <PageHeader breadcrumbs={CRUMBS} title={<Skeleton className="h-4 w-40" />} />
            <div className="mb-5 flex gap-1">
                {Array.from({ length: 4 }).map((_, i) => (
                    <Skeleton key={i} className="h-7 w-24 rounded-md" />
                ))}
            </div>
            <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_17rem] lg:gap-10">
                <div className="space-y-8">
                    <Skeleton className="h-64 w-full rounded-lg" />
                    <Skeleton className="h-40 w-full rounded-lg" />
                </div>
                <div className="space-y-3">
                    {Array.from({ length: 7 }).map((_, i) => (
                        <Skeleton key={i} className="h-5 w-full" />
                    ))}
                </div>
            </div>
        </div>
    );
}

// ---- API keys ----

const KEY_STATUS_TONE: Record<string, Tone> = {
    active: "success",
    revoked: "danger",
    expired: "neutral",
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
        <Section
            description="Keys the workspace minted for the public API. The secret is never shown; revoking is immediate and is recorded in the admin audit log with the reason."
        >
            {keysQ.isLoading ? (
                <Skeleton className="h-32 w-full rounded-lg" />
            ) : keysQ.error ? (
                <ErrorState error={keysQ.error} title="Failed to load API keys" onRetry={() => keysQ.refetch()} />
            ) : keys.length === 0 ? (
                <div className="surface-lit rounded-xl border border-border bg-card">
                    <EmptyState
                        icon={KeyRound}
                        title="No API keys"
                        hint="This workspace has not created any API keys."
                        className="py-10"
                    />
                </div>
            ) : (
                <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
                    <div className="overflow-x-auto">
                        <table className={TABLE}>
                            <thead>
                                <tr className="border-b border-border">
                                    <th className={TH}>Name</th>
                                    <th className={TH}>Key</th>
                                    <th className={TH}>Status</th>
                                    <th className={TH}>User</th>
                                    <th className={TH}>Last used</th>
                                    <th className={cn(TH, "text-right")}>Requests 7d</th>
                                    <th className={TH}>Expires</th>
                                    <th className={TH}>Created</th>
                                    <th className={cn(TH, "text-right")} />
                                </tr>
                            </thead>
                            <tbody>
                                {keys.map((k) => (
                                    <tr key={k.id} className={cn(TR, "group")}>
                                        <td className={cn(TD, "font-medium text-foreground")}>
                                            {k.name || <span className="font-normal text-subtle-foreground">untitled</span>}
                                        </td>
                                        <td className={cn(TD, "whitespace-nowrap font-mono text-[12px] text-muted-foreground")}>
                                            {k.key_prefix}…{k.key_suffix}
                                        </td>
                                        <td className={TD}>
                                            <StatusBadge tone={KEY_STATUS_TONE[k.status] ?? "neutral"} dot>
                                                {k.status}
                                            </StatusBadge>
                                        </td>
                                        <td className={TD}>{k.user_email || k.user_id.slice(0, 8)}</td>
                                        <td className={cn(TD, "whitespace-nowrap text-muted-foreground")} title={fmtDateTime(k.last_used_at)}>
                                            {k.last_used_at ? fmtAgo(k.last_used_at) : "never"}
                                        </td>
                                        <td className={cn(TD, "text-right tabular-nums")}>{k.requests_last_7d.toLocaleString()}</td>
                                        <td className={cn(TD, "whitespace-nowrap tabular-nums text-muted-foreground")}>
                                            {k.expires_at ? fmtDate(k.expires_at) : "never"}
                                        </td>
                                        <td className={cn(TD, "whitespace-nowrap tabular-nums text-muted-foreground")}>{fmtDate(k.created_at)}</td>
                                        <td className={cn(TD, "text-right")}>
                                            {k.status === "active" && (
                                                <Button
                                                    size="xs"
                                                    variant="ghost"
                                                    className={cn(
                                                        TONE_TEXT.danger,
                                                        "hover:bg-red-500/10 hover:text-red-700 focus-visible:opacity-100 opacity-100 md:opacity-0 md:group-hover:opacity-100 dark:hover:text-red-300",
                                                    )}
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
                        <Label htmlFor="revoke-reason" className="text-xs font-medium text-muted-foreground">
                            Reason <span className="font-normal text-subtle-foreground">(optional, goes to the audit log)</span>
                        </Label>
                        <Input
                            id="revoke-reason"
                            value={reason}
                            onChange={(e) => setReason(e.target.value)}
                            placeholder="e.g. leaked in a public repository"
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
        </Section>
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
        <Section
            description="Endpoints the workspace registered for event delivery. Consecutive failures and the last failure reason are what the delivery loop sees; drops are events skipped because the endpoint was disabled or over its failure ceiling."
        >
            {hooksQ.isLoading ? (
                <Skeleton className="h-32 w-full rounded-lg" />
            ) : hooksQ.error ? (
                <ErrorState error={hooksQ.error} title="Failed to load webhooks" onRetry={() => hooksQ.refetch()} />
            ) : hooks.length === 0 ? (
                <div className="surface-lit rounded-xl border border-border bg-card">
                    <EmptyState
                        icon={Webhook}
                        title="No webhooks"
                        hint="This workspace has no webhook endpoints."
                        className="py-10"
                    />
                </div>
            ) : (
                <div className="overflow-hidden surface-lit rounded-xl border border-border bg-card">
                    <div className="overflow-x-auto">
                        <table className={TABLE}>
                            <thead>
                                <tr className="border-b border-border">
                                    <th className={TH}>Endpoint</th>
                                    <th className={TH}>Enabled</th>
                                    <th className={TH}>Events</th>
                                    <th className={cn(TH, "text-right")}>Failures in a row</th>
                                    <th className={TH}>Last success</th>
                                    <th className={TH}>Last failure</th>
                                    <th className={cn(TH, "text-right")}>7d delivered / failed / drops</th>
                                </tr>
                            </thead>
                            <tbody>
                                {hooks.map((h: AdminWebhookEndpointRow) => (
                                    <tr key={h.id} className={cn(TR, "align-top")}>
                                        <td className={TD}>
                                            <div className="max-w-xs truncate font-mono text-[12px] text-foreground" title={h.url}>
                                                {h.url}
                                            </div>
                                            {h.description && (
                                                <div className="mt-0.5 text-xs text-muted-foreground">{h.description}</div>
                                            )}
                                        </td>
                                        <td className={TD}>
                                            <StatusBadge tone={h.enabled ? "success" : "neutral"} dot>
                                                {h.enabled ? "enabled" : "disabled"}
                                            </StatusBadge>
                                        </td>
                                        <td className={TD}>
                                            <div className="flex max-w-xs flex-wrap gap-1">
                                                {(h.event_types ?? []).length === 0 ? (
                                                    <span className="text-muted-foreground">all</span>
                                                ) : (
                                                    (h.event_types ?? []).map((t) => (
                                                        <span
                                                            key={t}
                                                            className="inline-flex h-5 items-center rounded-[4px] border border-border bg-muted/50 px-1.5 font-mono text-[10.5px] text-muted-foreground"
                                                        >
                                                            {t}
                                                        </span>
                                                    ))
                                                )}
                                            </div>
                                        </td>
                                        <td
                                            className={cn(
                                                TD,
                                                "text-right tabular-nums",
                                                h.consecutive_failures > 0 ? cn("font-medium", TONE_TEXT.danger) : "text-muted-foreground",
                                            )}
                                        >
                                            {h.consecutive_failures}
                                        </td>
                                        <td className={cn(TD, "whitespace-nowrap text-muted-foreground")} title={fmtDateTime(h.last_success_at)}>
                                            {h.last_success_at ? fmtAgo(h.last_success_at) : "never"}
                                        </td>
                                        <td className={TD}>
                                            <div className="whitespace-nowrap text-muted-foreground" title={fmtDateTime(h.last_failure_at)}>
                                                {h.last_failure_at ? fmtAgo(h.last_failure_at) : "never"}
                                            </div>
                                            {h.last_failure_reason && (
                                                <div className={cn("mt-0.5 max-w-xs truncate text-xs", TONE_TEXT.danger)} title={h.last_failure_reason}>
                                                    {h.last_failure_reason}
                                                </div>
                                            )}
                                        </td>
                                        <td className={cn(TD, "whitespace-nowrap text-right tabular-nums")}>
                                            {h.deliveries_last_7d.toLocaleString()}
                                            <span className="text-subtle-foreground"> / </span>
                                            <span className={h.failed_last_7d > 0 ? TONE_TEXT.danger : ""}>{h.failed_last_7d.toLocaleString()}</span>
                                            <span className="text-subtle-foreground"> / </span>
                                            <span className={h.drops_last_7d > 0 ? TONE_TEXT.warning : ""}>{h.drops_last_7d.toLocaleString()}</span>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                </div>
            )}
        </Section>
    );
}
