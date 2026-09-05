// useFeatureAccess — single source of truth for "can this org do X".
//
// Plan ladder lifted from warmbly-web/src/pages/pricing.astro:
//
//   free       → no active subscription
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

import useSubscription from "@/lib/api/hooks/app/subscription/useSubscription";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
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
    /** False on a deployment running without a billing provider: every gate
     *  below is open and billing/referral surfaces do not apply. */
    billing: boolean;
    /** Active subscription on any paid tier. */
    paid: boolean;
    /** Hosted workspace without a subscription: only mailboxes, the Warmbly
     *  Cloud link and settings are open; everything else waits for a plan. */
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
    /** Webhook endpoints — Business+. */
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

    const planId = ((sub.data?.plan?.name ?? currentOrg?.plan ?? "free").toLowerCase()) as PlanID;
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
        loading: sub.isPending || authConfig.isLoading,
        status,
        plan,
        billing: true,
        paid: isPaid,
        locked: !sub.isPending && !authConfig.isLoading && !isPaid,
        // Unified inbox is included on the free trial and on every paid tier,
        // so gate it on having an active/trialing subscription (isPaid) rather
        // than the plan-name → catalog map, which doesn't recognise server plan
        // names like "Pro" / "Free Trial" and would wrongly lock paid orgs.
        hasInbox: isPaid,
        hasAdvanced: isPaid && isAtLeast(plan, "business"),
        hasIsolatedSending: isPaid && isAtLeast(plan, "business"),
        hasRealtime: true,
        hasBulkOps: isPaid && isAtLeast(plan, "starter"),
        hasTeam: isPaid && isAtLeast(plan, "starter"),
        hasWebhooks: isPaid && isAtLeast(plan, "business"),
        isOwner,
        canManage,
    };
}
