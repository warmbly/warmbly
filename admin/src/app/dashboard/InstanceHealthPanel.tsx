// Overview shows only problems; Setup and health also shows informational context.

import { Link } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, ArrowRight, ExternalLink, Info, XCircle, type LucideIcon } from "lucide-react";
import { Callout, StatusBadge } from "@/components/ui/kit";
import { Button } from "@/components/ui/button";
import { useConfirm } from "@/components/ConfirmDialog";
import { docsUrl } from "@/lib/docs";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { INSTANCE_HEALTH_KEY, useInstanceHealth } from "@/hooks/useInstanceHealth";
import { AdminPerm } from "@/lib/auth/permissions";
import { TONE_PANEL, TONE_TEXT, type Tone } from "@/lib/tones";
import { cn } from "@/lib/utils";
import type { CheckSeverity, InstanceCheck } from "@/lib/api/client/admin/instance";
import { deleteExpiredInvitations } from "@/lib/api/client/admin/instance";

const SEVERITY_ORDER: CheckSeverity[] = ["error", "warning", "info"];

interface SeverityStyle {
    label: string;
    heading: string;
    icon: LucideIcon;
    tone: Tone;
}

const SEVERITY_STYLES: Record<CheckSeverity, SeverityStyle> = {
    error: { label: "Error", heading: "Errors", icon: XCircle, tone: "danger" },
    warning: { label: "Warning", heading: "Warnings", icon: AlertTriangle, tone: "warning" },
    info: { label: "Info", heading: "Worth knowing", icon: Info, tone: "info" },
};

// A severity the frontend does not know yet still renders, as info.
function toneFor(severity: CheckSeverity): SeverityStyle {
    return SEVERITY_STYLES[severity] ?? SEVERITY_STYLES.info;
}

// The whole list, errors first. Used by the Setup and health page.
export function InstanceFindings({ checks }: { checks: InstanceCheck[] }) {
    return (
        <div className="space-y-6">
            {SEVERITY_ORDER.map((severity) => {
                const group = checks.filter((c) => c.severity === severity);
                if (group.length === 0) return null;
                const tone = toneFor(severity);
                return (
                    <section key={severity}>
                        <div className="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                            <tone.icon className={cn("size-3.5", TONE_TEXT[tone.tone])} />
                            {tone.heading}
                            <span className="tabular-nums text-subtle-foreground">{group.length}</span>
                        </div>
                        <ul className="divide-y divide-border overflow-hidden surface-lit rounded-xl border border-border bg-card">
                            {group.map((check) => (
                                <FindingRow key={`${check.id}:${check.target ?? ""}`} check={check} />
                            ))}
                        </ul>
                    </section>
                );
            })}
        </div>
    );
}

function FindingRow({ check }: { check: InstanceCheck }) {
    const tone = toneFor(check.severity);
    const canGrantAdmin = useAdminPerm(AdminPerm.GrantAdminAccess);
    const canManageOrganizations = useAdminPerm(AdminPerm.ManageOrganizations);
    return (
        <li className="flex items-start gap-3 px-4 py-3">
            <tone.icon className={cn("mt-0.5 size-4 shrink-0", TONE_TEXT[tone.tone])} />
            <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                    <span className="text-[13px] font-medium text-foreground">{check.title}</span>
                    {check.target && (
                        <span className="max-w-[18rem] truncate rounded-[4px] border border-border bg-muted/60 px-1.5 py-px font-mono text-[11px] text-muted-foreground">
                            {check.target}
                        </span>
                    )}
                    <StatusBadge tone={tone.tone}>{tone.label}</StatusBadge>
                </div>
                <p className="mt-1 break-words text-[13px] leading-relaxed text-muted-foreground">{check.message}</p>
                <div className="mt-2 flex flex-wrap items-center gap-3">
                    {check.id === "single_platform_admin" && canGrantAdmin && (
                        <Link to="/admins" className="inline-flex items-center gap-1 text-xs font-medium text-[var(--admin-accent-strong)] hover:underline">
                            Manage admins
                            <ArrowRight className="size-3" />
                        </Link>
                    )}
                    {check.id === "expired_invitations" && canManageOrganizations && (
                        <ExpiredInvitationsCleanup />
                    )}
                    {check.docs && (
                        <a
                            href={docsUrl(check.docs)}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex items-center gap-1 text-xs font-medium text-[var(--admin-accent-strong)] hover:underline"
                        >
                            Learn more
                            <ExternalLink className="size-3" />
                        </a>
                    )}
                    <code className="font-mono text-[11px] text-subtle-foreground">{check.id}</code>
                </div>
            </div>
        </li>
    );
}

function ExpiredInvitationsCleanup() {
    const confirm = useConfirm();
    const qc = useQueryClient();
    const cleanup = useMutation({
        mutationFn: deleteExpiredInvitations,
        onSuccess: async () => {
            toast.success("Expired invitations removed");
            await qc.invalidateQueries({ queryKey: INSTANCE_HEALTH_KEY });
        },
        onError: (e: Error) => toast.error(e.message || "Could not remove expired invitations"),
    });

    async function onCleanup() {
        const ok = await confirm({
            title: "Remove expired invitations?",
            description: "This permanently removes expired invitation records across all workspaces. Active invitations, accounts and workspace members are unchanged.",
            confirmLabel: "Remove expired invitations",
            destructive: true,
        });
        if (ok) cleanup.mutate();
    }

    return (
        <Button size="sm" variant="outline" onClick={onCleanup} disabled={cleanup.isPending}>
            {cleanup.isPending ? "Removing..." : "Remove expired invitations"}
        </Button>
    );
}

const OVERVIEW_LIMIT = 4;

export function InstanceProblemsPanel() {
    const canRead = useAdminPerm(AdminPerm.ViewAnalytics);
    const healthQ = useInstanceHealth({ enabled: canRead });
    const checks = healthQ.data?.checks ?? [];
    const actionable = checks.filter((c) => c.severity === "error" || c.severity === "warning");

    if (!canRead) return null;
    if (healthQ.isError || (healthQ.data && actionable.length === 0)) return (
        <Callout className="mb-4" tone={healthQ.isError ? "warning" : "info"} title={healthQ.isError ? "Configuration findings unavailable" : "No configuration errors or warnings reported"}>
            {healthQ.isError ? "Could not refresh findings. This is not an all-clear." : "Absent findings do not establish check coverage or operational health."}
            {healthQ.isError && healthQ.data && ` Previous response listed ${actionable.length} errors or warnings; it is not a fresh verdict.`}
            {" "}<Link to="/health" className="font-medium hover:underline">Setup and health</Link>
        </Callout>
    );
    if (actionable.length === 0) return null;

    const errors = actionable.filter((c) => c.severity === "error").length;
    const shown = actionable.slice(0, OVERVIEW_LIMIT);
    const panelTone: Tone = errors > 0 ? "danger" : "warning";
    const HeadIcon = errors > 0 ? XCircle : AlertTriangle;

    return (
        <div className={cn("mb-8 overflow-hidden rounded-lg border", TONE_PANEL[panelTone])}>
            <div className="flex items-center justify-between gap-3 px-4 pt-3 pb-2">
                <div className="flex min-w-0 items-center gap-2 text-[13px] font-medium text-foreground">
                    <HeadIcon className={cn("size-4 shrink-0", TONE_TEXT[panelTone])} />
                    {actionable.length === 1
                        ? "1 problem needs attention"
                        : `${actionable.length} problems need attention`}
                </div>
                <Link
                    to="/health"
                    className="inline-flex shrink-0 items-center gap-1 rounded px-1 py-0.5 text-xs font-medium text-[var(--admin-accent-strong)] hover:underline"
                >
                    Setup and health
                    <ArrowRight className="size-3" />
                </Link>
            </div>
            <ul className="px-4 pb-3">
                {shown.map((check) => {
                    const tone = toneFor(check.severity);
                    return (
                        <li
                            key={`${check.id}:${check.target ?? ""}`}
                            className="flex items-start gap-2 py-0.5 text-[13px] leading-relaxed"
                        >
                            <tone.icon className={cn("mt-[3px] size-3.5 shrink-0", TONE_TEXT[tone.tone])} />
                            <span className="min-w-0">
                                <span className="text-foreground">{check.title}</span>
                                {check.target && <span className="text-muted-foreground"> ({check.target})</span>}
                            </span>
                        </li>
                    );
                })}
            </ul>
            {actionable.length > shown.length && (
                <div className="border-t border-border/60 px-4 py-2 text-xs text-muted-foreground">
                    {actionable.length - shown.length} more on Setup and health.
                </div>
            )}
        </div>
    );
}
