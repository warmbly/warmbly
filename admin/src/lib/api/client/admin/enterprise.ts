// /admin/enterprise/inquiries — sales-pipeline triage.

import { Request } from "@/lib/api/client";
import type {
    EnterpriseInquiry,
    UpdateEnterpriseInquiryRequest,
} from "@/lib/api/models/admin";

export function listEnterpriseInquiries(
    status?: string,
    limit = 50,
): Promise<{ data: EnterpriseInquiry[] }> {
    const usp = new URLSearchParams();
    if (status) usp.set("status", status);
    usp.set("limit", String(limit));
    return Request({
        method: "GET",
        url: `/admin/enterprise/inquiries?${usp.toString()}`,
        authorization: true,
    });
}

export function getEnterpriseInquiry(id: string): Promise<EnterpriseInquiry> {
    return Request({
        method: "GET",
        url: `/admin/enterprise/inquiries/${id}`,
        authorization: true,
    });
}

export function updateEnterpriseInquiry(
    id: string,
    body: UpdateEnterpriseInquiryRequest,
): Promise<EnterpriseInquiry> {
    return Request({
        method: "PATCH",
        url: `/admin/enterprise/inquiries/${id}`,
        authorization: true,
        data: body,
    });
}
