import Request from "../../Request";
import type ClientReport from "@/lib/api/models/app/reports/ClientReport";
import type {
    ClientReportInput,
    ClientReportLinkCreated,
    ClientReportPage,
    ClientReportRange,
    ClientReportScheduleInput,
    ClientReportView,
} from "@/lib/api/models/app/reports/ClientReport";

export async function listClientReports(cursor?: string, limit = 100, signal?: AbortSignal): Promise<ClientReportPage> {
    const params = new URLSearchParams({ limit: String(limit) });
    if (cursor) params.set("cursor", cursor);
    const res = await Request<ClientReportPage>({ method: "GET", url: `/reports?${params.toString()}`, authorization: true, signal });
    return { data: res.data ?? [], pagination: res.pagination ?? { next_cursor: null, has_more: false } };
}

export async function getClientReport(id: string, signal?: AbortSignal): Promise<ClientReport> {
    return Request<ClientReport>({ method: "GET", url: `/reports/${id}`, authorization: true, signal });
}

export async function previewClientReport(id: string, range?: ClientReportRange, signal?: AbortSignal): Promise<ClientReportView> {
    const qs = range ? `?range=${range}` : "";
    return Request<ClientReportView>({ method: "GET", url: `/reports/${id}/preview${qs}`, authorization: true, signal });
}

export async function createClientReport(data: ClientReportInput): Promise<ClientReport> {
    return Request<ClientReport>({ method: "POST", url: "/reports", data, authorization: true });
}

export async function updateClientReport(id: string, data: ClientReportInput): Promise<ClientReport> {
    return Request<ClientReport>({ method: "PATCH", url: `/reports/${id}`, data, authorization: true });
}

export async function deleteClientReport(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/reports/${id}`, authorization: true });
}

export async function createClientReportLink(id: string, expiresInDays?: number): Promise<ClientReportLinkCreated> {
    const data = expiresInDays ? { expires_in_days: expiresInDays } : {};
    return Request<ClientReportLinkCreated>({ method: "POST", url: `/reports/${id}/link`, data, authorization: true });
}

export async function revokeClientReportLink(id: string): Promise<ClientReport> {
    return Request<ClientReport>({ method: "DELETE", url: `/reports/${id}/link`, authorization: true });
}

export async function setClientReportSchedule(id: string, data: ClientReportScheduleInput): Promise<ClientReport> {
    return Request<ClientReport>({ method: "PUT", url: `/reports/${id}/schedule`, data, authorization: true });
}

export async function deleteClientReportSchedule(id: string): Promise<ClientReport> {
    return Request<ClientReport>({ method: "DELETE", url: `/reports/${id}/schedule`, authorization: true });
}
