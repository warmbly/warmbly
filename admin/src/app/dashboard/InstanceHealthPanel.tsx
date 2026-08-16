// The findings from GET /admin/instance/health, rendered two ways: the full
// severity-grouped list on the Setup and health page, and a short problems
// strip at the top of Overview. Only non-ok checks come back, so anything
// rendered here is something an operator has to decide about.

import { Link } from "react-router-dom";
import {
    AlertTriangle,
    ArrowRight,
    ExternalLink,
    Info,
    XCircle,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { docsUrl } from "@/lib/docs";
import { useAdminPerm } from "@/hooks/useAdminPerm";
import { useInstanceHealth } from "@/hooks/useInstanceHealth";
import { AdminPerm } from "@/lib/auth/permissions";
import type { CheckSeverity, InstanceCheck } from "@/lib/api/client/admin/instance";

const SEVERITY_ORDER: CheckSeverity[] = ["error", "warning", "info"];

interface SeverityStyle {
    label: string;
    heading: string;
    icon: React.ComponentType<{ className?: string }>;
    iconClass: string;
    row: string;
    chip: string;
}

const SEVERITY_STYLES: Record<CheckSeverity, SeverityStyle> = {
    error: {
        label: "Error",
        heading: "Errors",
        icon: XCircle,
        iconClass: "text-red-600",
        row: "border-red-200 bg-red-50/60",
        chip: "border-red-300 bg-red-50 text-red-700",
    },
    warning: {
        label: "Warning",
        heading: "Warnings",
        icon: AlertTriangle,
        iconClass: "text-amber-600",
        row: "border-amber-200 bg-amber-50/60",
        chip: "border-amber-300 bg-amber-50 text-amber-700",
    },
    info: {
        label: "Info",
        heading: "Worth knowing",
        icon: Info,
        iconClass: "text-sky-600",
        row: "border-sky-200 bg-sky-50/50",
        chip: "border-sky-300 bg-sky-50 text-sky-700",
    },
};

// A severity the frontend does not know yet still renders, as info.
function toneFor(severity: CheckSeverity): SeverityStyle {
    return SEVERITY_STYLES[severity] ?? SEVERITY_STYLES.info;
}

// The whole list, errors first. Used by the Setup and health page.
export function InstanceFindings({ checks }: { checks: InstanceCheck[] }) {
    return (
        <div className="space-y-5">
            {SEVERITY_ORDER.map((severity) => {
                const group = checks.filter((c) => c.severity === severity);
                if (group.length === 0) return null;
                const tone = toneFor(severity);
                return (
                    <section key={severity}>
                        <div className="mb-2 flex items-center gap-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                            <tone.icon className={`size-3.5 ${tone.iconClass}`} />
                            {tone.heading}
                            <span className="tabular-nums text-muted-foreground/70">
                                {group.length}
                            </span>
                        </div>
                        <ul className="space-y-2">
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
    return (
        <li className={`rounded-lg border p-3 ${tone.row}`}>
            <div className="flex items-start gap-3">
                <tone.icon className={`mt-0.5 size-4 shrink-0 ${tone.iconClass}`} />
                <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-semibold text-foreground">
                            {check.title}
                        </span>
                        {check.target && (
                            <Badge
                                variant="outline"
                                className="max-w-[18rem] truncate bg-white/70 text-[10px] font-normal"
                            >
                                {check.target}
                            </Badge>
                        )}
                        <Badge variant="outline" className={`text-[10px] ${tone.chip}`}>
                            {tone.label}
                        </Badge>
                    </div>
                    <p className="mt-1 text-[13px] leading-relaxed text-foreground/80 break-words">
                        {check.message}
                    </p>
                    <div className="mt-2 flex flex-wrap items-center gap-3">
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
                        <code className="text-[10px] text-muted-foreground/70">{check.id}</code>
                    </div>
                </div>
            </div>
        </li>
    );
}

const OVERVIEW_LIMIT = 4;

// Overview's problems strip. Silent when the instance is clean or when the
// caller cannot read the endpoint, so it never becomes noise on the page.
export function InstanceProblemsPanel() {
    const canRead = useAdminPerm(AdminPerm.ViewAnalytics);
    const healthQ = useInstanceHealth({ enabled: canRead });
    const checks = healthQ.data?.checks ?? [];
    const actionable = checks.filter(
        (c) => c.severity === "error" || c.severity === "warning",
    );

    if (healthQ.isError || actionable.length === 0) return null;

    const errors = actionable.filter((c) => c.severity === "error").length;
    const shown = actionable.slice(0, OVERVIEW_LIMIT);

    return (
        <div
            className={`mb-6 rounded-lg border p-3 ${
                errors > 0 ? "border-red-200 bg-red-50/60" : "border-amber-200 bg-amber-50/60"
            }`}
        >
            <div className="mb-2 flex items-center justify-between gap-2">
                <div className="text-sm font-semibold text-foreground">
                    {actionable.length === 1
                        ? "1 problem needs attention"
                        : `${actionable.length} problems need attention`}
                </div>
                <Link
                    to="/health"
                    className="inline-flex items-center gap-1 text-xs font-medium text-[var(--admin-accent-strong)] hover:underline"
                >
                    Setup and health
                    <ArrowRight className="size-3" />
                </Link>
            </div>
            <ul className="space-y-1.5">
                {shown.map((check) => {
                    const tone = toneFor(check.severity);
                    return (
                        <li
                            key={`${check.id}:${check.target ?? ""}`}
                            className="flex items-start gap-2 text-[13px] leading-relaxed"
                        >
                            <tone.icon className={`mt-0.5 size-3.5 shrink-0 ${tone.iconClass}`} />
                            <span className="min-w-0">
                                <span className="font-medium text-foreground">{check.title}</span>
                                {check.target && (
                                    <span className="text-muted-foreground"> ({check.target})</span>
                                )}
                            </span>
                        </li>
                    );
                })}
            </ul>
            {actionable.length > shown.length && (
                <div className="mt-2 text-xs text-muted-foreground">
                    {actionable.length - shown.length} more on Setup and health.
                </div>
            )}
        </div>
    );
}
