// useFeatureAccess — single source of truth for "can this org do X".
//
// Plan ladder lifted from warmbly-web/src/pages/pricing.astro:
//
//   free       → no active subscription
//   warmup     → $15/mo, premium pool and no mailbox cap; no sending
//   starter    → $29/mo, 150 sends/day
//   grow       → $89/mo, 3k sends/day
//   business   → $329/mo, 15k sends/day + isolated sending   (featured)
//   enterprise → custom, 15k+ sends/day + isolated sending
//
// Gates here decide which dashboard features show up in the sidebar
// + which surfaces render the LockedSurface overlay. The minimum
// unlock plan should always match what we promise on the pricing
// page.
//
// A deployment with billing disabled (BILLING_PROVIDER=none, the self-host
// default) unlocks everything server-side, so it is unlocked here too and the
// subscription row is ignored: it would otherwise report a free trial that
// nothing enforces.

import { useEffect, useState } from "react";
import type Subscription from "@/lib/api/models/app/subscription/Subscription";
import useSubscription from "@/lib/api/hooks/app/subscription/useSubscription";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { useAppStore } from "@/stores";
import { PERMISSION_BITS, hasPermission } from "@/lib/permissions";
import {
    WARMUP_PLAN_ID,
    PARTNERSHIP_PLAN_ID,
    TEST_PLAN_ID,
    AGENCY_PLAN_TIERS,
    getPlan,
    isAtLeast,
    isWarmupOnlyPlan,
    type PlanID,
} from "@/lib/plans";

export type Plan = PlanID;

export interface FeatureAccess {
    loading: boolean;
    status?: Subscription["status"];
    plan: PlanID;
    /** The plan's own name when it is not a catalog plan (an agency plan), shown instead of the catalog label. */
    planLabel?: string;
    /** The plan is a grant (an operator's or an agency's), so the workspace's own trial status does not apply. */
    managed?: boolean;
    /** False on a deployment running without a billing provider: every gate
     *  below is open and billing surfaces do not apply. */
    billing: boolean;
    /** Active subscription on any paid tier, the Warmup plan included. */
    paid: boolean;
    /** On a warming-only plan (Warmup, Partnership): the premium pool and no mailbox cap, nothing else.
     *  Every surface a free workspace cannot use stays locked. */
    warmupOnly: boolean;
    /** On the Partnership plan: warming only, plus the API, placement tests and its own OAuth apps. */
    partnership: boolean;
    /** Unpaid workspace that kept the free allowance: 10 mailboxes in the free pool. A new one gets none. */
    keptFreeWarmup: boolean;
    /** Unpaid workspace inside its free trial: sending, the inbox and a small mailbox allowance, until trialEndsAt. */
    trial: boolean;
    /** Unpaid workspace whose free trial is over: sending and warmup are paused until it picks a plan. */
    trialEnded: boolean;
    trialEndsAt: Date | null;
    /** Hosted workspace without a plan that sends (free, or the Warmup plan):
     *  only mailboxes, the Warmbly Cloud link and settings are open; everything
     *  else waits for a plan. */
    locked: boolean;
    /** Unified inbox — free trial and Starter+. */
    hasInbox: boolean;
    /** Advanced outreach (AB tests, custom rules) — Business+. */
    hasAdvanced: boolean;
    /** Sending on infrastructure bound to this org alone — Business+. */
    hasIsolatedSending: boolean;
    /** Realtime websocket events — every tier, baseline. */
    hasRealtime: boolean;
    /** Bulk import/edit on contacts — Starter+. */
    hasBulkOps: boolean;
    /** Team invitations — Starter+. */
    hasTeam: boolean;
    /** Webhook endpoints — Grow+, so both agency plans. */
    hasWebhooks: boolean;
    /** Convenience: viewer is the current org's owner. */
    isOwner: boolean;
    /** Owner OR admin. */
    canManage: boolean;
}

export default function useFeatureAccess(): FeatureAccess {
    const sub = useSubscription();
    const authConfig = useAuthConfig();
    const currentOrg = useAppStore((s) => s.currentOrganization);
    // The trial gate reads the clock, so the caller re-renders when it ends even if no data changes.
    const rawTrialEnd = sub.data?.free_trial_ends_at ? Date.parse(sub.data.free_trial_ends_at) : NaN;
    useRerenderAt(Number.isNaN(rawTrialEnd) ? null : rawTrialEnd);

    const isOwner = currentOrg?.role === "owner";
    // Permission-aware: a custom role carrying MANAGE_TEAM unlocks the
    // same management surfaces as the built-in admin role.
    const canManage =
        currentOrg?.role === "owner" ||
        currentOrg?.role === "admin" ||
        hasPermission(currentOrg?.permissions, PERMISSION_BITS.MANAGE_TEAM);

    // Only the confirmed answer counts; the fallback keeps billing on so an
    // unreachable backend never reads as an unlocked one.
    const billingOff = !!authConfig.data && authConfig.data.billing_enabled === false;
    if (billingOff) {
        return {
            loading: false,
            status: "active",
            plan: "enterprise",
            billing: false,
            paid: true,
            warmupOnly: false,
            partnership: false,
            keptFreeWarmup: false,
            trial: false,
            trialEnded: false,
            trialEndsAt: null,
            locked: false,
            hasInbox: true,
            hasAdvanced: true,
            hasIsolatedSending: true,
            hasRealtime: true,
            hasBulkOps: true,
            hasTeam: true,
            hasWebhooks: true,
            isOwner,
            canManage,
        };
    }

    const testGrant = sub.data?.managed === true && sub.data.plan?.id === TEST_PLAN_ID;
    const agencyPlan = sub.data?.plan?.id ? AGENCY_PLAN_TIERS[sub.data.plan.id] : undefined;
    const planId = testGrant
        ? "business"
        : agencyPlan
        ? agencyPlan.tier
        : sub.data?.plan?.id === WARMUP_PLAN_ID
        ? "warmup"
        : sub.data?.plan?.id === PARTNERSHIP_PLAN_ID
        ? "partnership"
        : ((sub.data?.plan?.name ?? currentOrg?.plan ?? "free").toLowerCase()) as PlanID;
    const plan = getPlan(planId).id;
    const status = sub.data?.status;

    // Real paid status comes from Stripe via /subscription. While
    // that's in flight, fall back to the org row's plan field so a
    // paying customer doesn't see "Locked" for a beat on first load.
    const subSaysPaid = status === "active" || status === "trialing";
    // A plan an operator granted never touches Stripe, so `status` stays
    // whatever it was ("incomplete" on a workspace that never subscribed).
    // Deciding paid from status alone locks a workspace that is entitled to
    // everything, which is what the plan pill and the balance already
    // show correctly. The server resolves expiry, so this is just a read.
    const managed = sub.data?.managed === true;
    const orgImpliesPaid =
        sub.isPending && !!currentOrg?.plan && currentOrg.plan.toLowerCase() !== "free";
    const isPaid = subSaysPaid || managed || orgImpliesPaid;
    // The Warmup and Partnership plans are paid, but pay for warming only; the
    // server refuses sending, the inbox and AI to them as it does to a free workspace.
    const warmupOnly = isPaid && isWarmupOnlyPlan(plan);
    const productPaid = isPaid && !warmupOnly;
    const trialEndsAt = !isPaid && sub.data?.free_trial_ends_at ? new Date(sub.data.free_trial_ends_at) : null;
    const trial = !!trialEndsAt && trialEndsAt.getTime() > Date.now();
    // The trial opens what the server opens to it: sending, the inbox, contacts and campaigns.
    const opened = productPaid || trial;

    return {
        loading: sub.isPending || authConfig.isLoading,
        status,
        plan,
        planLabel: agencyPlan?.label,
        managed,
        billing: true,
        paid: isPaid,
        warmupOnly,
        partnership: warmupOnly && plan === "partnership",
        keptFreeWarmup: !isPaid && sub.data?.keeps_free_warmup === true,
        trial,
        trialEnded: !!trialEndsAt && !trial,
        trialEndsAt,
        locked: !sub.isPending && !authConfig.isLoading && !opened,
        // Unified inbox is included on every plan that sends, so gate it on
        // the subscription (productPaid) rather than the plan-name → catalog
        // map, which doesn't recognise server plan names like "Pro" / "Free
        // Trial" and would wrongly lock paid orgs.
        hasInbox: opened,
        hasAdvanced: productPaid && isAtLeast(plan, "business"),
        hasIsolatedSending: productPaid && isAtLeast(plan, "business"),
        hasRealtime: true,
        hasBulkOps: productPaid && isAtLeast(plan, "starter"),
        hasTeam: productPaid && isAtLeast(plan, "starter"),
        hasWebhooks: productPaid && isAtLeast(plan, "grow"),
        isOwner,
        canManage,
    };
}

/** Whole days left in a trial, rounded up and never below 1 while it runs; every countdown reads this. */
export function trialDaysLeft(endsAt: Date, now = Date.now()): number {
    return Math.max(1, Math.ceil((endsAt.getTime() - now) / 86_400_000));
}

const DAY = 86_400_000;

/** trialDaysLeft that re-renders when the count drops, so every countdown on screen changes together. */
export function useTrialDaysLeft(endsAt: Date | null): number {
    const days = endsAt ? trialDaysLeft(endsAt) : 0;
    useRerenderAt(endsAt && days > 1 ? endsAt.getTime() - (days - 1) * DAY : null);
    return days;
}

// Re-renders the caller once `at` (epoch ms) has passed.
function useRerenderAt(at: number | null) {
    const [, bump] = useState(0);
    useEffect(() => {
        if (at == null) return;
        const wait = at - Date.now();
        if (wait <= 0) return;
        const timer = setTimeout(() => bump((n) => n + 1), Math.min(wait + 500, 2_000_000_000));
        return () => clearTimeout(timer);
    }, [at]);
}
