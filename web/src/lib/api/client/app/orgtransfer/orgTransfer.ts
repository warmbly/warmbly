import type {
    CreateOrgExportPayload,
    CreateOrgImportOptions,
    OrgExportJob,
    OrgImportJob,
    OrgImportPreflight,
    OrgTransferGroupsResponse,
} from "@/lib/api/models/app/orgtransfer/OrgTransfer";
import Client from "../../Client";
import Request from "../../Request";
import getToken from "@/lib/helper/getToken";

export async function getOrgTransferGroups(): Promise<OrgTransferGroupsResponse> {
    return await Request<OrgTransferGroupsResponse>({
        method: "GET",
        url: `/organization/current/transfer/groups`,
        authorization: true,
    });
}

export async function createOrgExport(data: CreateOrgExportPayload): Promise<OrgExportJob> {
    return await Request<OrgExportJob>({
        method: "POST",
        url: `/organization/current/export`,
        data,
        authorization: true,
    });
}

export async function listOrgExports(): Promise<OrgExportJob[]> {
    const res = await Request<{ data: OrgExportJob[] }>({
        method: "GET",
        url: `/organization/current/export`,
        authorization: true,
    });
    return res.data ?? [];
}

export async function deleteOrgExport(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/organization/current/export/${id}`,
        authorization: true,
    });
}

export async function listOrgImports(): Promise<OrgImportJob[]> {
    const res = await Request<{ data: OrgImportJob[] }>({
        method: "GET",
        url: `/organization/current/import`,
        authorization: true,
    });
    return res.data ?? [];
}

export async function preflightOrgImport(
    file: File,
    passphrase: string,
): Promise<OrgImportPreflight> {
    const form = new FormData();
    form.append("file", file);
    if (passphrase) form.append("passphrase", passphrase);
    return await Request<OrgImportPreflight>({
        method: "POST",
        url: `/organization/current/import/preflight`,
        data: form,
        authorization: true,
    });
}

export async function createOrgImport(
    file: File,
    options: CreateOrgImportOptions,
    passphrase: string,
): Promise<OrgImportJob> {
    const form = new FormData();
    form.append("file", file);
    form.append("options", JSON.stringify(options));
    if (passphrase) form.append("passphrase", passphrase);
    return await Request<OrgImportJob>({
        method: "POST",
        url: `/organization/current/import`,
        data: form,
        authorization: true,
    });
}

/**
 * Downloads a finished archive.
 *
 * The endpoint is bearer-authenticated, so a plain anchor href cannot fetch it.
 * The blob is pulled through the same client and handed to the browser as an
 * object URL, which also means the caller can show progress while it streams.
 */
export async function downloadOrgExport(id: string): Promise<{ blob: Blob; filename: string }> {
    const token = getToken();
    const res = await Client.request<Blob>({
        method: "GET",
        url: `/organization/current/export/${id}/download`,
        responseType: "blob",
        headers: token?.access_token
            ? { Authorization: `Bearer ${token.access_token}` }
            : undefined,
    });

    // Prefer the server's filename; fall back to something recognisable.
    const disposition = String(res.headers?.["content-disposition"] ?? "");
    const match = /filename="?([^"]+)"?/.exec(disposition);
    return {
        blob: res.data,
        filename: match?.[1] ?? `workspace-${id.slice(0, 8)}.warmbly.zip`,
    };
}
