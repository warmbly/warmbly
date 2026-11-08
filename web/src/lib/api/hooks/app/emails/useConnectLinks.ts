import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createConnectLink, listConnectLinks, listConnectLinkUses, revokeConnectLink } from "@/lib/api/client/app/emails/connectLinks";
import type { ConnectLinkList, NewConnectLink } from "@/lib/api/models/app/emails/ConnectLink";

// The audit spine invalidates this key on every create, revoke and client connect.
export const CONNECT_LINKS_KEY = ["emails", "connect-links"] as const;

export const connectLinksQuery = () =>
    queryOptions({
        queryKey: [...CONNECT_LINKS_KEY, "list"],
        queryFn: () => listConnectLinks({ limit: 100 }),
        staleTime: 30_000,
    });

export default function useConnectLinks(enabled = true) {
    return useQuery({ ...connectLinksQuery(), enabled });
}

export function useConnectLinkUses(id: string | null) {
    return useQuery({
        queryKey: [...CONNECT_LINKS_KEY, "uses", id],
        queryFn: () => listConnectLinkUses(id as string),
        enabled: !!id,
        staleTime: 30_000,
    });
}

export function useCreateConnectLink() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ body, idempotencyKey }: { body: NewConnectLink; idempotencyKey: string }) => createConnectLink(body, idempotencyKey),
        onSuccess: (created) => {
            qc.setQueryData<ConnectLinkList>(connectLinksQuery().queryKey, (prev) =>
                prev ? { ...prev, data: [created.link, ...prev.data.filter((l) => l.id !== created.link.id)] } : prev,
            );
        },
        onSettled: () => qc.invalidateQueries({ queryKey: CONNECT_LINKS_KEY }),
    });
}

// Revoking is predictable, so the row turns off at once and comes back if the server refuses.
export function useRevokeConnectLink() {
    const qc = useQueryClient();
    const key = connectLinksQuery().queryKey;
    return useMutation({
        mutationFn: (id: string) => revokeConnectLink(id),
        onMutate: async (id) => {
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData<ConnectLinkList>(key);
            if (prev) {
                const now = new Date().toISOString();
                qc.setQueryData<ConnectLinkList>(key, {
                    ...prev,
                    data: prev.data.map((l) => (l.id === id ? { ...l, status: "revoked", revoked_at: l.revoked_at ?? now } : l)),
                });
            }
            return { prev };
        },
        onError: (_e, _id, ctx) => {
            if (ctx?.prev) qc.setQueryData(key, ctx.prev);
        },
        onSettled: () => qc.invalidateQueries({ queryKey: CONNECT_LINKS_KEY }),
    });
}
