import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    adoptCloudMailbox,
    disconnectCloudLink,
    enrollCloudLinkMailbox,
    listCloudWorkspaceMailboxes,
    getCloudLinkStatus,
    listCloudLinkMailboxes,
    pollCloudLinkConnect,
    setCloudLinkMailboxLifecycle,
    startCloudLinkConnect,
    unenrollCloudLinkMailbox,
} from "@/lib/api/client/app/cloudlink/cloudLink";
import {
    approvePoolLinkCode,
    denyPoolLinkCode,
    describePoolLinkCode,
    getPoolLinkOffer,
    listPoolLinkInstances,
    startPoolLinkCheckout,
    revokePoolLinkInstance,
} from "@/lib/api/client/app/cloudlink/poolLink";

import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import { SENDING_DOMAINS_KEY } from "@/lib/api/hooks/app/emails/useSendingDomains";

export const CLOUD_LINK_KEY = ["cloud-link"];
export const POOL_LINK_KEY = ["pool-link"];

// Self-hosted side.

// Each read asks Warmbly Cloud, so a surface that only needs the redirect offer can take a longer staleTime.
export function useCloudLinkStatus(enabled = true, staleTime = 10_000) {
    const organization = useCurrentOrganization();
    return useQuery({ queryKey: [...CLOUD_LINK_KEY, organization.data?.id, "status"], queryFn: getCloudLinkStatus, enabled: enabled && !!organization.data?.id, staleTime });
}

export function useCloudLinkMailboxes(enabled = true) {
    const organization = useCurrentOrganization();
    return useQuery({ queryKey: [...CLOUD_LINK_KEY, organization.data?.id, "mailboxes"], queryFn: listCloudLinkMailboxes, enabled: enabled && !!organization.data?.id, staleTime: 10_000 });
}

export function useStartCloudLinkConnect() {
    return useMutation({ mutationFn: (cloudUrl?: string) => startCloudLinkConnect(cloudUrl) });
}

export function usePollCloudLinkConnect() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => pollCloudLinkConnect(),
        onSuccess: (res) => {
            if (res.status === "approved") void qc.invalidateQueries({ queryKey: CLOUD_LINK_KEY });
        },
    });
}

export function useDisconnectCloudLink() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (legacy: boolean) => disconnectCloudLink(legacy),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: CLOUD_LINK_KEY });
            void qc.invalidateQueries({ queryKey: ["emails"] });
            // Redirects Warmbly Cloud served stop with the link.
            void qc.invalidateQueries({ queryKey: SENDING_DOMAINS_KEY });
        },
    });
}

export function useEnrollCloudLinkMailbox() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => enrollCloudLinkMailbox(id),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: CLOUD_LINK_KEY });
            void qc.invalidateQueries({ queryKey: ["emails"] });
        },
    });
}

export function useUnenrollCloudLinkMailbox() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => unenrollCloudLinkMailbox(id),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: CLOUD_LINK_KEY });
            void qc.invalidateQueries({ queryKey: ["emails"] });
        },
    });
}

export function useCloudLinkMailboxLifecycle() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, action }: { id: string; action: "pause" | "resume" }) => setCloudLinkMailboxLifecycle(id, action),
        onSuccess: () => void qc.invalidateQueries({ queryKey: CLOUD_LINK_KEY }),
    });
}

export function useCloudWorkspaceMailboxes(enabled = true) {
    const organization = useCurrentOrganization();
    return useQuery({ queryKey: [...CLOUD_LINK_KEY, organization.data?.id, "workspace-mailboxes"], queryFn: listCloudWorkspaceMailboxes, enabled: enabled && !!organization.data?.id, staleTime: 10_000 });
}

export function useAdoptCloudMailbox() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => adoptCloudMailbox(id),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: CLOUD_LINK_KEY });
            void qc.invalidateQueries({ queryKey: ["emails"] });
        },
    });
}

// Cloud side.

export function usePoolLinkCode(code: string) {
    return useQuery({
        queryKey: [...POOL_LINK_KEY, "code", code],
        queryFn: () => describePoolLinkCode(code),
        enabled: code.replace(/[^A-Za-z0-9]/g, "").length === 8,
        retry: 0,
    });
}

export function useApprovePoolLinkCode() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ code, organizationId }: { code: string; organizationId: string }) => approvePoolLinkCode(code, organizationId),
        onSuccess: () => void qc.invalidateQueries({ queryKey: POOL_LINK_KEY }),
    });
}

export function useDenyPoolLinkCode() {
    return useMutation({ mutationFn: (code: string) => denyPoolLinkCode(code) });
}

export function usePoolLinkInstances(enabled = true) {
    const organization = useCurrentOrganization();
    return useQuery({ queryKey: [...POOL_LINK_KEY, organization.data?.id, "instances"], queryFn: listPoolLinkInstances, enabled: enabled && !!organization.data?.id, staleTime: 10_000 });
}

export function useRevokePoolLinkInstance() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => revokePoolLinkInstance(id),
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: POOL_LINK_KEY });
            void qc.invalidateQueries({ queryKey: ["emails"] });
        },
    });
}

export function usePoolLinkOffer(enabled = true) {
    return useQuery({ queryKey: [...POOL_LINK_KEY, "offer"], queryFn: getPoolLinkOffer, enabled, staleTime: 60_000 });
}

export function useStartPoolLinkCheckout() {
    return useMutation({ mutationFn: (interval: "month" | "year") => startPoolLinkCheckout(interval) });
}
