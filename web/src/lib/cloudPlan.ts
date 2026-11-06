// Warmbly Cloud is a paid plan for self-hosted instances: a free workspace keeps what it already enrolled and adds nothing.

import type { PoolLinkPlan } from "@/lib/api/models/app/cloudlink/CloudLink";

/** Warmbly Cloud's dashboard; the link API (`default_cloud_url`) lives on another host. */
export const CLOUD_APP_URL = "https://app.warmbly.com";

/** The community server, the only help a self-hosted instance gets. */
export const COMMUNITY_URL = "https://dc.warmbly.com";

/** True when the linked Cloud workspace takes no new mailboxes until it subscribes. Older clouds never say so. */
export function cloudAddsNeedSubscription(plan: PoolLinkPlan | undefined): boolean {
    return plan?.subscription_required === true;
}
