import Request from "../../Request";
import type Form from "@/lib/api/models/app/forms/Form";
import type { FormsConfig, FormSubmission, FormWrite } from "@/lib/api/models/app/forms/Form";
import type { FormStats, FormStatsRange } from "@/lib/api/models/app/forms/FormStats";
import type CampaignFormStats from "@/lib/api/models/app/forms/CampaignFormStats";
import type FormsDomainStatus from "@/lib/api/models/app/forms/FormsDomain";

/** The three uploadable brand assets on a form. */
export type FormAssetKind = "logo" | "cover" | "background";

export async function listForms(): Promise<Form[]> {
    const res = await Request<{ data: Form[] }>({ method: "GET", url: "/forms", authorization: true });
    return res.data ?? [];
}

export async function getForm(id: string): Promise<Form> {
    return await Request<Form>({ method: "GET", url: `/forms/${id}`, authorization: true });
}

export async function getFormsConfig(): Promise<FormsConfig> {
    return await Request<FormsConfig>({ method: "GET", url: "/forms/config", authorization: true });
}

export async function createForm(name: string): Promise<Form> {
    return await Request<Form>({ method: "POST", url: "/forms", data: { name }, authorization: true });
}

export async function updateForm(id: string, data: FormWrite): Promise<Form> {
    return await Request<Form>({ method: "PATCH", url: `/forms/${id}`, data, authorization: true });
}

export async function deleteForm(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/forms/${id}`, authorization: true });
}

/** Where the next submissions page starts: the opaque cursor, or the raw
 * `created_at` an older backend that issues no cursor pages by. */
export type FormSubmissionsPageParam = { cursor: string } | { before: string };

export interface FormSubmissionsPage {
    data: FormSubmission[];
    has_more: boolean;
    next: FormSubmissionsPageParam | null;
}

export async function listFormSubmissions(
    id: string,
    limit = 50,
    page?: FormSubmissionsPageParam,
): Promise<FormSubmissionsPage> {
    const params = new URLSearchParams({ limit: String(limit) });
    if (page && "cursor" in page) params.set("cursor", page.cursor);
    if (page && "before" in page) params.set("before", page.before);
    const res = await Request<{
        data: FormSubmission[];
        has_more: boolean;
        pagination?: { next_cursor: string | null; has_more: boolean };
    }>({
        method: "GET",
        url: `/forms/${id}/submissions?${params.toString()}`,
        authorization: true,
    });
    const data = res.data ?? [];
    const hasMore = res.pagination?.has_more ?? res.has_more ?? false;
    const last = data[data.length - 1];
    let next: FormSubmissionsPageParam | null = null;
    if (res.pagination?.next_cursor) next = { cursor: res.pagination.next_cursor };
    // Verbatim, never through Date: that would drop the microseconds.
    else if (hasMore && last) next = { before: last.created_at };
    return { data, has_more: hasMore, next };
}

export async function deleteFormSubmission(formId: string, submissionId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/forms/${formId}/submissions/${submissionId}`, authorization: true });
}

export async function getFormStats(id: string, range: FormStatsRange): Promise<FormStats> {
    return await Request<FormStats>({ method: "GET", url: `/forms/${id}/stats?range=${range}`, authorization: true });
}

/** Mints (or re-fetches, it is an upsert) the personalized link for a contact. */
export async function getFormContactLink(formId: string, contactId: string): Promise<{ url: string }> {
    return await Request<{ url: string }>({
        method: "GET",
        url: `/forms/${formId}/links/${contactId}`,
        authorization: true,
    });
}

export async function uploadFormImage(formId: string, kind: FormAssetKind, file: Blob): Promise<Form> {
    const fd = new FormData();
    fd.append("file", file);
    return Request<Form>({ method: "POST", url: `/forms/${formId}/assets/${kind}`, data: fd, authorization: true });
}

export async function deleteFormImage(formId: string, kind: FormAssetKind): Promise<Form> {
    return Request<Form>({ method: "DELETE", url: `/forms/${formId}/assets/${kind}`, authorization: true });
}

/** How the forms linked from a campaign performed for its recipients. */
export async function listCampaignForms(campaignId: string): Promise<CampaignFormStats[]> {
    const res = await Request<{ data: CampaignFormStats[] }>({
        method: "GET",
        url: `/campaigns/${campaignId}/forms`,
        authorization: true,
    });
    return res.data ?? [];
}

export async function getFormsDomain(): Promise<FormsDomainStatus> {
    return await Request<FormsDomainStatus>({ method: "GET", url: "/forms/domain", authorization: true });
}

/** Saving resolves the record too, so save and verify are one action. */
export async function setFormsDomain(domain: string): Promise<FormsDomainStatus> {
    return await Request<FormsDomainStatus>({
        method: "PUT",
        url: "/forms/domain",
        data: { forms_domain: domain },
        authorization: true,
    });
}

export async function verifyFormsDomain(): Promise<FormsDomainStatus> {
    return await Request<FormsDomainStatus>({ method: "POST", url: "/forms/domain/verify", authorization: true });
}
