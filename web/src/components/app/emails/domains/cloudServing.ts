// Whether Warmbly Cloud can serve this instance's root redirects: self-hosted, linked, and with room for one more.
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { useInstanceAdmin } from "@/hooks/usePermission";
import { useCloudLinkStatus } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import type { PoolLinkRedirectOffer } from "@/lib/api/models/app/cloudlink/CloudLink";
import type { RedirectServer } from "@/lib/api/models/app/emails/SendingDomain";

export interface CloudServing {
    /** Only a self-hosted instance's administrator picks, since it forwards credentials off the server; Warmbly Cloud always serves its own. */
    choosable: boolean;
    connected: boolean;
    offer: PoolLinkRedirectOffer | null;
    /** Cloud would take one more redirect for this instance. */
    canServe: boolean;
}

export function useCloudServing(): CloudServing {
    const auth = useAuthConfig();
    const instanceAdmin = useInstanceAdmin();
    const choosable = !!auth.data?.self_hosted && instanceAdmin.allowed;
    const status = useCloudLinkStatus(choosable, 60_000);
    const offer = choosable ? (status.data?.info?.redirects ?? null) : null;
    const connected = choosable && !!status.data?.connected;
    return {
        choosable,
        connected,
        offer,
        canServe: connected && !!status.data?.link?.organization_id && !!offer?.available && offer.used < offer.limit,
    };
}

/** What a new redirect is served from when nobody picked: Cloud when it can, since it needs nothing on this server. */
export function defaultServer(cloud: CloudServing): RedirectServer {
    return cloud.choosable && cloud.canServe ? "cloud" : "instance";
}
