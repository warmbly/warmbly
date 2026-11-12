import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
    deleteOrganizationAvatar,
    uploadOrganizationAvatar,
} from "@/lib/api/client/app/avatar/uploadUserAvatar";
import type Organization from "@/lib/api/models/app/organizations/Organization";
import { useAppStore } from "@/stores";

// The workspace avatar is read from the persisted zustand org pointer, which
// is fed by the /organization list. Patch the store and the cached queries
// with the server's answer right away, then invalidate so the list refetch
// (and OrgGate's setOrganizations) settles on the same value. Every patch is
// keyed on the org the mutation targeted, so a workspace switch mid-flight
// cannot stamp the avatar onto the wrong org.
function applyOrgAvatar(qc: QueryClient, orgId: string | null, avatarUrl: string | null) {
    if (orgId) {
        // Generic: the store rows and the query rows are different org shapes.
        const patch = <T extends { id: string; avatar_url?: string | null }>(o: T): T =>
            o.id === orgId ? { ...o, avatar_url: avatarUrl } : o;
        const store = useAppStore.getState();
        const current = store.memberOrganization ?? store.currentOrganization;
        const list = store.organizations;
        if (list.some((o) => o.id === orgId)) {
            // setOrganizations adopts the matching row as the current org.
            store.setOrganizations(list.map(patch));
        } else if (current && current.id === orgId) {
            // List not loaded yet: patch the pointer directly, since
            // setOrganizations would null it without a matching row.
            store.setCurrentOrganization({ ...current, avatar_url: avatarUrl });
        }
        qc.setQueryData<Organization[]>(["organizations", "list"], (old) => (old ? old.map(patch) : old));
        qc.setQueryData<Organization>(["organizations", "current"], (old) => (old ? patch(old) : old));
    }
    qc.invalidateQueries({ queryKey: ["organizations"] });
}

// The org a mutation is about is the one selected when it starts, not when
// it resolves.
const targetOrgId = () => useAppStore.getState().currentOrganization?.id ?? null;

export function useUploadOrgAvatar() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (blob: Blob) => uploadOrganizationAvatar(blob),
        onMutate: () => ({ orgId: targetOrgId() }),
        onSuccess: (res, _blob, ctx) => applyOrgAvatar(qc, ctx?.orgId ?? null, res.avatar_url),
    });
}

export function useDeleteOrgAvatar() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => deleteOrganizationAvatar(),
        onMutate: () => ({ orgId: targetOrgId() }),
        onSuccess: (_res, _vars, ctx) => applyOrgAvatar(qc, ctx?.orgId ?? null, null),
    });
}
