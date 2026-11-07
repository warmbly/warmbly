// One view of the Warmbly Cloud pool link for the dashboard: whether this is
// a self-hosted instance, whether it is linked, and which mailboxes the cloud
// warms. Members who manage mailboxes see the link and use it for their own
// mailboxes (`manageable`); linking it is the instance administrator's.

import { useCallback, useMemo } from "react";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { useInstanceAdmin, usePermission } from "@/hooks/usePermission";
import { useCloudLinkMailboxes, useCloudLinkStatus } from "@/lib/api/hooks/app/cloudlink/useCloudLink";
import type { CloudLinkMailboxRow, PoolLinkPlan } from "@/lib/api/models/app/cloudlink/CloudLink";

export default function useCloudPool() {
    const authConfig = useAuthConfig();
    const selfHosted = authConfig.data?.self_hosted === true;
    const instanceAdmin = useInstanceAdmin();
    const manageEmails = usePermission("MANAGE_EMAILS");
    const manageable = selfHosted && (instanceAdmin.allowed || manageEmails);
    const status = useCloudLinkStatus(manageable);
    const connected = manageable && status.data?.connected === true;
    const mailboxes = useCloudLinkMailboxes(connected);

    const byId = useMemo(() => {
        const m = new Map<string, CloudLinkMailboxRow>();
        for (const r of mailboxes.data ?? []) m.set(r.id, r);
        return m;
    }, [mailboxes.data]);

    const enrolledCount = useMemo(() => (mailboxes.data ?? []).filter((r) => r.enrolled).length, [mailboxes.data]);
    const plan: PoolLinkPlan | undefined = status.data?.info?.plan;
    const rowFor = useCallback((id: string) => byId.get(id), [byId]);

    return {
        selfHosted,
        manageable,
        connected,
        workspaceConnected: connected && !!status.data?.link?.organization_id,
        reachable: status.data?.reachable === true,
        orgName: status.data?.link?.organization_name ?? "",
        plan,
        enrolledCount,
        rowFor,
        isEnrolled: (id: string) => byId.get(id)?.enrolled === true,
        loading: manageable && (status.isPending || (connected && mailboxes.isPending)),
        refreshing: manageable && (status.isFetching || (connected && mailboxes.isFetching)),
        unavailable: manageable && (status.isError || (connected && mailboxes.isError)),
        observedAt: connected ? mailboxes.dataUpdatedAt : 0,
    };
}
