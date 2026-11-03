// PlanPill — colored subscription badge for the top header.
//
// Mirrors what the workspace is currently paying for, with colors
// drawn from `lib/plans.PLAN_ACCENT_CLASSES` so the marketing site
// and the dashboard always agree on what "Starter" looks like.

import { Link } from "@tanstack/react-router";
import { SparklesIcon } from "lucide-react";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useCloudPool from "@/hooks/useCloudPool";
import { usePermission } from "@/hooks/usePermission";
import { PLAN_ACCENT_CLASSES, getPlan } from "@/lib/plans";
import { cn } from "@/lib/utils";
import { useAppStore } from "@/stores";
import { cloudAddsNeedSubscription } from "@/lib/cloudPlan";

export function PlanPill() {
    const access = useFeatureAccess();
    const pool = useCloudPool();
    // The Warmbly Cloud page is manage-settings gated and only exists for a linked instance; everyone else lands on their profile.
    const cloudTo = usePermission("MANAGE_SETTINGS") && pool.connected ? "/app/settings/warmbly-cloud" : "/app/settings/profile";
    const isTest = useAppStore((s) => s.currentOrganization?.category === "test");
    if (isTest) {
        return (
            <Badge
                to="/app/settings/profile"
                className="bg-sky-50 text-sky-700 border-sky-100"
                dot="bg-sky-500"
                label="Test"
                title="Dedicated tester workspace"
            />
        );
    }
    if (access.loading) {
        return (
            <div className="h-6 w-16 rounded border border-slate-200 bg-slate-100 animate-pulse" />
        );
    }

    // No billing provider: nothing is metered here. The one plan a
    // self-hosted instance can hold is its Warmbly Cloud pool plan, so once
    // it is linked the pill shows that instead of the deployment mode.
    if (!access.billing) {
        if (pool.selfHosted && pool.connected && pool.plan) {
            const premium = pool.plan.tier === "paid";
            return (
                <Badge
                    to={cloudTo}
                    className={premium ? "bg-sky-50 text-sky-700 border-sky-100" : "bg-slate-100 text-slate-600 border-slate-200"}
                    dot={premium ? "bg-sky-500" : "bg-slate-400"}
                    label={premium ? "Cloud · Premium" : "Cloud · Free"}
                    title={
                        premium
                            ? "Warmbly Cloud Premium: better deliverability for every mailbox"
                            : cloudAddsNeedSubscription(pool.plan)
                              ? `Warmbly Cloud: ${pool.plan.enrolled} mailboxes keep warming. Subscribe to add more`
                              : `Warmbly Cloud: ${pool.plan.enrolled} of ${pool.plan.mailbox_limit ?? 10} free pool mailboxes used`
                    }
                    icon={premium}
                />
            );
        }
        return (
            <Badge
                to={cloudTo}
                className="bg-indigo-50 text-indigo-700 border-indigo-100"
                dot="bg-indigo-500"
                label="Self-hosted"
                title="Self-hosted: every feature is unlocked, with no support from Warmbly"
                icon
            />
        );
    }

    // Status overrides — "Past due" and "Canceled" take precedence
    // over the underlying plan colour so the user notices.
    if (access.status === "past_due") {
        return (
            <Badge
                to={access.isOwner ? "/app/settings/billing" : "/app/settings/roles"}
                className="bg-red-50 text-red-700 border-red-100"
                dot="bg-red-500"
                label="Past due"
                title="Subscription is past due — open billing"
            />
        );
    }
    if (access.status === "canceled") {
        return (
            <Badge
                to={access.isOwner ? "/app/settings/billing" : "/app/settings/roles"}
                className="bg-slate-100 text-slate-500 border-slate-200"
                dot="bg-slate-400"
                label="Canceled"
                title="Subscription is canceled"
            />
        );
    }

    const plan = getPlan(access.plan);
    const accent = PLAN_ACCENT_CLASSES[plan.accent];
    const label = access.status === "trialing" ? `${plan.label} · Trial` : plan.label;
    const dest = access.isOwner ? "/app/settings/billing" : "/app/settings/roles";

    return (
        <Badge
            to={dest}
            className={accent.pill}
            dot={accent.dot}
            label={label}
            title={`Workspace plan: ${plan.label}${access.isOwner ? " — manage" : ""}`}
            icon={plan.id === "enterprise" || plan.id === "business"}
        />
    );
}

function Badge({
    to,
    className,
    dot,
    label,
    title,
    icon,
}: {
    to: string;
    className: string;
    dot: string;
    label: string;
    title: string;
    icon?: boolean;
}) {
    return (
        <Link
            to={to}
            title={title}
            className={cn(
                "inline-flex items-center gap-1.5 h-6 px-2 rounded border text-[11px] font-semibold uppercase tracking-[0.08em] transition-colors hover:brightness-105",
                className,
            )}
        >
            {icon ? (
                <SparklesIcon className="w-2.5 h-2.5" />
            ) : (
                <span className={cn("size-1.5 rounded-full", dot)} />
            )}
            {label}
        </Link>
    );
}
