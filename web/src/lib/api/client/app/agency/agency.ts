import Request from "../../Request";
import type { AgenciesResult, Agency, AgencyAIBalance, AgencyAITransferResult, AgencyClient, AgencyClientPatch, AgencyInvitation, AgencyMember, AgencyPlanChangePreview, AgencyPlansResult, AgencyRole, AgencyStaffResult, AgencyUsage } from "@/lib/api/models/app/agency/Agency";

const idem = () => ({ "Idempotency-Key": crypto.randomUUID() });

export const listAgencies = () => Request<AgenciesResult>({ method: "GET", url: "/agency", authorization: true });

export const listAgencyPlans = () => Request<AgencyPlansResult>({ method: "GET", url: "/agency/plans", authorization: true });

export const createAgency = (name: string) =>
    Request<Agency>({ method: "POST", url: "/agency", data: { name }, headers: idem(), authorization: true });

export const renameAgency = (agencyId: string, name: string) =>
    Request<Agency>({ method: "PATCH", url: `/agency/${agencyId}`, data: { name }, authorization: true });

export const listAgencyStaff = (agencyId: string) =>
    Request<AgencyStaffResult>({ method: "GET", url: `/agency/${agencyId}/members`, authorization: true });

export const addAgencyStaff = (agencyId: string, email: string, role: AgencyRole) =>
    Request<{ member: AgencyMember | null; invitation: AgencyInvitation | null }>({
        method: "POST",
        url: `/agency/${agencyId}/members`,
        data: { email, role },
        headers: idem(),
        authorization: true,
    });

export const updateAgencyStaff = (agencyId: string, userId: string, role: AgencyRole) =>
    Request<AgencyMember>({ method: "PATCH", url: `/agency/${agencyId}/members/${userId}`, data: { role }, authorization: true });

export const removeAgencyStaff = (agencyId: string, userId: string) =>
    Request<void>({ method: "DELETE", url: `/agency/${agencyId}/members/${userId}`, authorization: true });

export const cancelAgencyInvitation = (agencyId: string, invitationId: string) =>
    Request<void>({ method: "DELETE", url: `/agency/${agencyId}/invitations/${invitationId}`, authorization: true });

export const listAgencyClients = (agencyId: string) =>
    Request<{ data: AgencyClient[] }>({ method: "GET", url: `/agency/${agencyId}/clients`, authorization: true });

export const createAgencyClient = (agencyId: string, name: string, timezone?: string) =>
    Request<AgencyClient>({
        method: "POST",
        url: `/agency/${agencyId}/clients`,
        data: { name, timezone },
        headers: idem(),
        authorization: true,
    });

export const attachAgencyClient = (agencyId: string, organizationId: string) =>
    Request<AgencyClient>({
        method: "POST",
        url: `/agency/${agencyId}/clients/attach`,
        data: { organization_id: organizationId },
        headers: idem(),
        authorization: true,
    });

// Only the fields sent change; null removes that cap.
export const updateAgencyClient = (agencyId: string, orgId: string, patch: AgencyClientPatch) =>
    Request<AgencyClient>({
        method: "PATCH",
        url: `/agency/${agencyId}/clients/${orgId}`,
        data: patch,
        authorization: true,
    });

export const getAgencyAIBalance = (agencyId: string) =>
    Request<AgencyAIBalance>({ method: "GET", url: `/agency/${agencyId}/ai-balance`, authorization: true });

// The key is the caller's, so a retried submit of one dialog moves the money once.
export const transferAgencyAI = (agencyId: string, orgId: string, amountUsd: number, idempotencyKey: string) =>
    Request<AgencyAITransferResult>({
        method: "POST",
        url: `/agency/${agencyId}/clients/${orgId}/ai-balance`,
        data: { amount_usd: amountUsd },
        headers: { "Idempotency-Key": idempotencyKey },
        authorization: true,
    });

export const agencyAITopup = (agencyId: string, amountUsd: number) =>
    Request<{ checkout_url: string }>({
        method: "POST",
        url: `/agency/${agencyId}/billing/ai-topup`,
        data: { amount_usd: amountUsd },
        headers: idem(),
        authorization: true,
    });

export const detachAgencyClient = (agencyId: string, orgId: string) =>
    Request<void>({ method: "DELETE", url: `/agency/${agencyId}/clients/${orgId}`, authorization: true });

export const getAgencyUsage = (agencyId: string) =>
    Request<AgencyUsage>({ method: "GET", url: `/agency/${agencyId}/usage`, authorization: true });

export const agencyCheckout = (agencyId: string, planId: string, interval: "month" | "year") =>
    Request<{ checkout_url: string }>({
        method: "POST",
        url: `/agency/${agencyId}/billing/checkout`,
        data: { plan_id: planId, interval },
        headers: idem(),
        authorization: true,
    });

export const previewAgencyPlanChange = (agencyId: string, planId: string, interval: "month" | "year") =>
    Request<AgencyPlanChangePreview>({
        method: "GET",
        url: `/agency/${agencyId}/billing/change/preview?plan_id=${encodeURIComponent(planId)}&interval=${interval}`,
        authorization: true,
    });

export const changeAgencyPlan = (
    agencyId: string,
    planId: string,
    interval: "month" | "year",
    quote?: Pick<AgencyPlanChangePreview, "proration_date" | "from_price_id">,
) =>
    Request<Agency>({
        method: "POST",
        url: `/agency/${agencyId}/billing/change`,
        data: { plan_id: planId, interval, proration_date: quote?.proration_date, from_price_id: quote?.from_price_id },
        headers: idem(),
        authorization: true,
    });

export const agencyTopup = (agencyId: string) =>
    Request<{ checkout_url: string }>({ method: "POST", url: `/agency/${agencyId}/billing/topup`, data: {}, headers: idem(), authorization: true });

export const agencyPortal = (agencyId: string) =>
    Request<{ url: string }>({ method: "POST", url: `/agency/${agencyId}/billing/portal`, data: {}, authorization: true });
