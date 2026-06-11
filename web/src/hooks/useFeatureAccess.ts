// useFeatureAccess — single source of truth for "can this org do X".
//
// Plan ladder lifted from warmbly-web/src/pages/pricing.astro:
//
//   free       → no active subscription
//   starter    → $29/mo, 150 sends/day
//   grow       → $89/mo, 3k sends/day
//   business   → $329/mo, 15k sends/day + dedicated IPs   (featured)
//   enterprise → custom, 15k+ sends/day + dedicated IPs
//
// Gates here decide which dashboard features show up in the sidebar
// + which surfaces render the LockedSurface overlay. The minimum
// unlock plan should always match what we promise on the pricing
// page.

import useSubscription from "@/lib/api/hooks/app/subscription/useSubscription";
import { useAppStore } from "@/stores";
import { PERMISSION_BITS, hasPermission } from "@/lib/permissions";
import {
    getPlan,
    isAtLeast,
    type PlanID,
} from "@/lib/plans";

export type Plan = PlanID;

export interface FeatureAccess {
    loading: boolean;
    status?: "active" | "canceled" | "past_due" | "trialing" | "incomplete";
    plan: PlanID;
    /** Active subscription on any paid tier. */
    paid: boolean;
    /** Unified inbox — free trial and Starter+. */
    hasInbox: boolean;
    /** Advanced outreach (AB tests, custom rules) — Business+. */
    hasAdvanced: boolean;
    /** Dedicated IP pool — Business+. */
    hasDedicatedIps: boolean;
    /** Realtime websocket events — every tier, baseline. */
    hasRealtime: boolean;
    /** Bulk import/edit on contacts — Starter+. */
    hasBulkOps: boolean;
    /** Team invitations — Starter+. */
    hasTeam: boolean;
    /** Webhook endpoints — Business+. */
    hasWebhooks: boolean;
    /** Convenience: viewer is the current org's owner. */
    isOwner: boolean;
    /** Owner OR admin. */
    canManage: boolean;
}

export default function useFeatureAccess(): FeatureAccess {
    const sub = useSubscription();
    const currentOrg = useAppStore((s) => s.currentOrganization);

    const planId = ((sub.data?.plan_name ?? currentOrg?.plan ?? "free").toLowerCase()) as PlanID;
    const plan = getPlan(planId).id;
    const status = sub.data?.status;

    // Real paid status comes from Stripe via /subscription. While
    // that's in flight, fall back to the org row's plan field so a
    // paying customer doesn't see "Locked" for a beat on first load.
    const subSaysPaid = status === "active" || status === "trialing";
    const orgImpliesPaid =
        sub.isPending && !!currentOrg?.plan && currentOrg.plan.toLowerCase() !== "free";
    const isPaid = subSaysPaid || orgImpliesPaid;

    return {
        loading: sub.isPending,
        status,
        plan,
        paid: isPaid,
        // Unified inbox is included on the free trial and on every paid tier,
        // so gate it on having an active/trialing subscription (isPaid) rather
        // than the plan-name → catalog map, which doesn't recognise server plan
        // names like "Pro" / "Free Trial" and would wrongly lock paid orgs.
        hasInbox: isPaid,
        hasAdvanced: isPaid && isAtLeast(plan, "business"),
        hasDedicatedIps: isPaid && isAtLeast(plan, "business"),
        hasRealtime: true,
        hasBulkOps: isPaid && isAtLeast(plan, "starter"),
        hasTeam: isPaid && isAtLeast(plan, "starter"),
        hasWebhooks: isPaid && isAtLeast(plan, "business"),
        isOwner: currentOrg?.role === "owner",
        // Permission-aware: a custom role carrying MANAGE_TEAM unlocks the
        // same management surfaces as the built-in admin role.
        canManage:
            currentOrg?.role === "owner" ||
            currentOrg?.role === "admin" ||
            hasPermission(currentOrg?.permissions, PERMISSION_BITS.MANAGE_TEAM),
    };
}
