// useFeatureAccess — single source of truth for "can this org do X".
//
// Reads from the subscription hook and returns boolean gates that
// the page surfaces consult. Centralizing this means a future plan
// change only touches this file; pages stay declarative.
//
//   const access = useFeatureAccess();
//   if (!access.hasInbox) return <LockedSurface feature="Inbox" ... />;

import useSubscription from "@/lib/api/hooks/app/subscription/useSubscription";
import { useAppStore } from "@/stores";

export type Plan = "free" | "pro" | "premium" | "enterprise" | string;

export interface FeatureAccess {
    loading: boolean;
    /** Underlying subscription status — undefined while loading. */
    status?: "active" | "canceled" | "past_due" | "trialing" | "incomplete";
    plan: Plan;
    /** True when the user can be expected to have paid features. */
    paid: boolean;
    /** Inbox / unified mailbox — paid-only per product policy. */
    hasInbox: boolean;
    /** Advanced outreach (AB tests, advanced sequences). */
    hasAdvanced: boolean;
    /** Realtime websocket events. */
    hasRealtime: boolean;
    /** Bulk operations on contacts, campaigns. */
    hasBulkOps: boolean;
    /** Team invitations + multiple seats. */
    hasTeam: boolean;
    /** Custom webhooks. */
    hasWebhooks: boolean;
    /** Convenience: whether the viewer is the current org's owner. */
    isOwner: boolean;
}

export default function useFeatureAccess(): FeatureAccess {
    const sub = useSubscription();
    const currentOrg = useAppStore((s) => s.currentOrganization);

    const plan: Plan = (sub.data?.plan_name ?? currentOrg?.plan ?? "free").toLowerCase();
    const status = sub.data?.status;
    const isPaid = status === "active" || status === "trialing";
    const isPro = isPaid && plan !== "free";
    const isPremium = isPaid && (plan === "premium" || plan === "enterprise");
    const isEnterprise = isPaid && plan === "enterprise";

    return {
        loading: sub.isPending,
        status,
        plan,
        paid: isPaid,
        hasInbox: isPro,
        hasAdvanced: isPremium,
        hasRealtime: true, // baseline feature
        hasBulkOps: isPro,
        hasTeam: isPro,
        hasWebhooks: isEnterprise,
        isOwner: currentOrg?.role === "owner",
    };
}
