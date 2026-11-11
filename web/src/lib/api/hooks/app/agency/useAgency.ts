import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import {
    addAgencyStaff,
    agencyAITopup,
    agencyCheckout,
    agencyPortal,
    agencyTopup,
    attachAgencyClient,
    cancelAgencyInvitation,
    createAgency,
    createAgencyClient,
    detachAgencyClient,
    getAgencyAIBalance,
    getAgencyUsage,
    listAgencies,
    listAgencyClients,
    listAgencyPlans,
    listAgencyStaff,
    removeAgencyStaff,
    renameAgency,
    transferAgencyAI,
    updateAgencyClient,
    updateAgencyStaff,
    changeAgencyPlan,
} from "@/lib/api/client/app/agency/agency";
import type { Agency, AgencyClient, AgencyClientPatch, AgencyRole, AgencyPlanChangePreview } from "@/lib/api/models/app/agency/Agency";
import { currentPortal, portalAgencies } from "@/lib/portal";

// Every agency query sits under ["agency"], which the audit spine invalidates.
export const agenciesQuery = queryOptions({
    queryKey: ["agency", "list"],
    // On a portal domain only the agency that owns it is offered.
    queryFn: async () => {
        const res = await listAgencies();
        return { ...res, data: portalAgencies(res.data, currentPortal()) };
    },
    staleTime: 30_000,
});

export const agencyPlansQuery = queryOptions({
    queryKey: ["agency", "plans"],
    queryFn: () => listAgencyPlans(),
    staleTime: 5 * 60_000,
});

export const agencyClientsQuery = (agencyId: string) =>
    queryOptions({
        queryKey: ["agency", agencyId, "clients"],
        queryFn: async () => (await listAgencyClients(agencyId)).data,
        staleTime: 15_000,
    });

export const agencyStaffQuery = (agencyId: string) =>
    queryOptions({
        queryKey: ["agency", agencyId, "staff"],
        queryFn: () => listAgencyStaff(agencyId),
        staleTime: 15_000,
    });

export const agencyUsageQuery = (agencyId: string) =>
    queryOptions({
        queryKey: ["agency", agencyId, "usage"],
        queryFn: () => getAgencyUsage(agencyId),
        staleTime: 30_000,
    });

export const agencyAIBalanceQuery = (agencyId: string) =>
    queryOptions({
        queryKey: ["agency", agencyId, "ai-balance"],
        queryFn: () => getAgencyAIBalance(agencyId),
        staleTime: 30_000,
    });

const SELECTED_KEY = "warmbly.agency";
// The storage event only reaches other tabs; this one tells pages in the same tab.
const SELECTED_EVENT = "warmbly:agency-selected";

/** The agency the agency pages show, remembered per browser. */
export function readSelectedAgencyId(): string | null {
    try {
        return localStorage.getItem(SELECTED_KEY);
    } catch {
        return null;
    }
}

export function rememberAgency(id: string) {
    try {
        localStorage.setItem(SELECTED_KEY, id);
    } catch {
        /* private mode keeps the choice for this tab only */
    }
    window.dispatchEvent(new CustomEvent<string>(SELECTED_EVENT, { detail: id }));
}

export function pickAgency(list: Agency[] | undefined, selected: string | null): Agency | undefined {
    if (!list?.length) return undefined;
    return list.find((a) => a.id === selected) ?? list[0];
}

export function useAgencies() {
    return useQuery(agenciesQuery);
}

export function useCurrentAgency() {
    const list = useAgencies();
    const [selected, setSelected] = useState<string | null>(() => readSelectedAgencyId());
    const agency = pickAgency(list.data?.data, selected);
    useEffect(() => {
        const onStorage = (e: StorageEvent) => {
            if (e.key === SELECTED_KEY) setSelected(e.newValue);
        };
        const onSelected = (e: Event) => setSelected((e as CustomEvent<string>).detail);
        window.addEventListener("storage", onStorage);
        window.addEventListener(SELECTED_EVENT, onSelected);
        return () => {
            window.removeEventListener("storage", onStorage);
            window.removeEventListener(SELECTED_EVENT, onSelected);
        };
    }, []);
    const select = (id: string) => {
        rememberAgency(id);
        setSelected(id);
    };
    return { ...list, agency, agencies: list.data?.data ?? [], billingEnabled: list.data?.billing_enabled ?? false, select };
}

export function useAgencyPlans() {
    return useQuery(agencyPlansQuery);
}

export function useAgencyClients(agencyId?: string) {
    return useQuery({ ...agencyClientsQuery(agencyId ?? ""), enabled: !!agencyId });
}

export function useAgencyStaff(agencyId?: string) {
    return useQuery({ ...agencyStaffQuery(agencyId ?? ""), enabled: !!agencyId });
}

export function useAgencyUsage(agencyId?: string) {
    return useQuery({ ...agencyUsageQuery(agencyId ?? ""), enabled: !!agencyId });
}

export function useAgencyAIBalance(agencyId?: string) {
    return useQuery({ ...agencyAIBalanceQuery(agencyId ?? ""), enabled: !!agencyId });
}

export function useCreateAgency() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (name: string) => createAgency(name),
        onSuccess: (a) => {
            rememberAgency(a.id);
            void qc.invalidateQueries({ queryKey: ["agency"] });
        },
    });
}

export function useRenameAgency(agencyId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (name: string) => renameAgency(agencyId, name),
        onMutate: async (name) => {
            await qc.cancelQueries({ queryKey: agenciesQuery.queryKey });
            const prev = qc.getQueryData(agenciesQuery.queryKey);
            qc.setQueryData(agenciesQuery.queryKey, (old) =>
                old ? { ...old, data: old.data.map((a) => (a.id === agencyId ? { ...a, name } : a)) } : old,
            );
            return { prev };
        },
        onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(agenciesQuery.queryKey, ctx.prev),
        onSettled: () => void qc.invalidateQueries({ queryKey: ["agency"] }),
    });
}

export function useAddAgencyStaff(agencyId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ email, role }: { email: string; role: AgencyRole }) => addAgencyStaff(agencyId, email, role),
        onSettled: () => void qc.invalidateQueries({ queryKey: ["agency", agencyId] }),
    });
}

export function useUpdateAgencyStaff(agencyId: string) {
    const qc = useQueryClient();
    const key = agencyStaffQuery(agencyId).queryKey;
    return useMutation({
        mutationFn: ({ userId, role }: { userId: string; role: AgencyRole }) => updateAgencyStaff(agencyId, userId, role),
        onMutate: async ({ userId, role }) => {
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData(key);
            qc.setQueryData(key, (old) => (old ? { ...old, data: old.data.map((m) => (m.user_id === userId ? { ...m, role } : m)) } : old));
            return { prev };
        },
        onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(key, ctx.prev),
        onSettled: () => void qc.invalidateQueries({ queryKey: ["agency", agencyId] }),
    });
}

export function useRemoveAgencyStaff(agencyId: string) {
    const qc = useQueryClient();
    const key = agencyStaffQuery(agencyId).queryKey;
    return useMutation({
        mutationFn: (userId: string) => removeAgencyStaff(agencyId, userId),
        onMutate: async (userId) => {
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData(key);
            qc.setQueryData(key, (old) => (old ? { ...old, data: old.data.filter((m) => m.user_id !== userId) } : old));
            return { prev };
        },
        onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(key, ctx.prev),
        onSettled: () => void qc.invalidateQueries({ queryKey: ["agency"] }),
    });
}

export function useCancelAgencyInvitation(agencyId: string) {
    const qc = useQueryClient();
    const key = agencyStaffQuery(agencyId).queryKey;
    return useMutation({
        mutationFn: (invitationId: string) => cancelAgencyInvitation(agencyId, invitationId),
        onMutate: async (invitationId) => {
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData(key);
            qc.setQueryData(key, (old) => (old ? { ...old, invitations: old.invitations.filter((i) => i.id !== invitationId) } : old));
            return { prev };
        },
        onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(key, ctx.prev),
        onSettled: () => void qc.invalidateQueries({ queryKey: ["agency", agencyId] }),
    });
}

function afterClientChange(qc: ReturnType<typeof useQueryClient>, agencyId: string) {
    void qc.invalidateQueries({ queryKey: ["agency"] });
    void qc.invalidateQueries({ queryKey: ["organizations"] });
    void qc.invalidateQueries({ queryKey: ["agency", agencyId] });
}

export function useCreateAgencyClient(agencyId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ name, timezone }: { name: string; timezone?: string }) => createAgencyClient(agencyId, name, timezone),
        onSettled: () => afterClientChange(qc, agencyId),
    });
}

export function useAttachAgencyClient(agencyId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (organizationId: string) => attachAgencyClient(agencyId, organizationId),
        onSettled: () => afterClientChange(qc, agencyId),
    });
}

export function useDetachAgencyClient(agencyId: string) {
    const qc = useQueryClient();
    const key = agencyClientsQuery(agencyId).queryKey;
    return useMutation({
        mutationFn: (orgId: string) => detachAgencyClient(agencyId, orgId),
        onMutate: async (orgId) => {
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData(key);
            qc.setQueryData(key, (old) => old?.filter((c) => c.organization_id !== orgId));
            return { prev };
        },
        onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(key, ctx.prev),
        onSettled: () => afterClientChange(qc, agencyId),
    });
}

// One client's caps, patched optimistically: only the fields in the patch change.
function useClientPatch<V>(agencyId: string, toPatch: (v: V) => { orgId: string; patch: AgencyClientPatch }) {
    const qc = useQueryClient();
    const key = agencyClientsQuery(agencyId).queryKey;
    return useMutation({
        mutationFn: (v: V) => {
            const { orgId, patch } = toPatch(v);
            return updateAgencyClient(agencyId, orgId, patch);
        },
        onMutate: async (v: V) => {
            const { orgId, patch } = toPatch(v);
            await qc.cancelQueries({ queryKey: key });
            const prev = qc.getQueryData(key);
            qc.setQueryData(key, (old) => old?.map((c: AgencyClient) => (c.organization_id === orgId ? { ...c, ...patch } : c)));
            return { prev };
        },
        onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(key, ctx.prev),
        onSettled: () => void qc.invalidateQueries({ queryKey: ["agency", agencyId] }),
    });
}

export function useUpdateAgencyClientLimit(agencyId: string) {
    return useClientPatch(agencyId, ({ orgId, limit }: { orgId: string; limit: number | null }) => ({
        orgId,
        patch: { monthly_send_limit: limit },
    }));
}

export function useUpdateAgencyClientAILimit(agencyId: string) {
    return useClientPatch(agencyId, ({ orgId, limitUsd }: { orgId: string; limitUsd: number | null }) => ({
        orgId,
        patch: { monthly_ai_limit_usd: limitUsd },
    }));
}

// The server decides whether the balance holds the amount, so nothing moves until it answers.
export function useTransferAgencyAI(agencyId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ orgId, amountUsd, key }: { orgId: string; amountUsd: number; key: string }) =>
            transferAgencyAI(agencyId, orgId, amountUsd, key),
        onSettled: () => {
            void qc.invalidateQueries({ queryKey: ["agency", agencyId] });
            void qc.invalidateQueries({ queryKey: ["subscription"] });
        },
    });
}

export function useAgencyAITopup(agencyId: string) {
    return useMutation({ mutationFn: (amountUsd: number) => agencyAITopup(agencyId, amountUsd) });
}

export function useAgencyCheckout(agencyId: string) {
    return useMutation({
        mutationFn: ({ planId, interval }: { planId: string; interval: "month" | "year" }) => agencyCheckout(agencyId, planId, interval),
    });
}

// The server decides the switch and its price, so nothing is patched before it answers.
export function useChangeAgencyPlan(agencyId: string) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ planId, interval, quote }: { planId: string; interval: "month" | "year"; quote?: AgencyPlanChangePreview }) =>
            changeAgencyPlan(agencyId, planId, interval, quote),
        // The open workspace may be one of the agency's clients, whose own plan just changed with it.
        onSettled: () => {
            void qc.invalidateQueries({ queryKey: ["agency"] });
            void qc.invalidateQueries({ queryKey: ["subscription"] });
        },
    });
}

export function useAgencyTopup(agencyId: string) {
    return useMutation({ mutationFn: () => agencyTopup(agencyId) });
}

export function useAgencyPortal(agencyId: string) {
    return useMutation({ mutationFn: () => agencyPortal(agencyId) });
}
