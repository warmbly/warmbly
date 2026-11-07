// /emails/vendors: inbox vendor accounts (InboxKit, Zapmail, ...) the workspace imports from.
import Request from "@/lib/api/client/Request";
import type { MailboxImport } from "@/lib/api/models/app/emails/MailboxImport";
import type {
    CreateVendorRequest,
    ListResponse,
    UpdateVendorRequest,
    VendorCatalogEntry,
    VendorConnection,
    VendorImportRequest,
    VendorMailbox,
    VendorReconnectMailbox,
} from "@/lib/api/models/app/emails/MailboxSources";

export async function listVendorCatalog(): Promise<ListResponse<VendorCatalogEntry>> {
    return await Request<ListResponse<VendorCatalogEntry>>({ method: "GET", url: "/emails/vendors/catalog", authorization: true });
}

export async function listVendorConnections(): Promise<ListResponse<VendorConnection>> {
    return await Request<ListResponse<VendorConnection>>({ method: "GET", url: "/emails/vendors", authorization: true });
}

// Verifies the key with the vendor before storing it.
export async function createVendorConnection(body: CreateVendorRequest): Promise<VendorConnection> {
    return await Request<VendorConnection>({ method: "POST", url: "/emails/vendors", data: body, authorization: true, timeout: 60_000 });
}

export async function updateVendorConnection(id: string, body: UpdateVendorRequest): Promise<VendorConnection> {
    return await Request<VendorConnection>({ method: "PATCH", url: `/emails/vendors/${id}`, data: body, authorization: true, timeout: 60_000 });
}

// Mailboxes it brought in stay connected.
export async function deleteVendorConnection(id: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/emails/vendors/${id}`, authorization: true });
}

// Asks the vendor for the account's mailboxes, so it can take a while.
export async function listVendorMailboxes(id: string): Promise<ListResponse<VendorMailbox>> {
    return await Request<ListResponse<VendorMailbox>>({
        method: "GET",
        url: `/emails/vendors/${id}/mailboxes`,
        authorization: true,
        timeout: 60_000,
    });
}

export async function importVendorMailboxes(id: string, body: VendorImportRequest): Promise<MailboxImport> {
    return await Request<MailboxImport>({
        method: "POST",
        url: `/emails/vendors/${id}/import`,
        data: body,
        authorization: true,
        timeout: 60_000,
    });
}

// Starts the vendor signing in again as the named linked Google mailboxes, or every one; the work goes on in the background.
export async function reconnectVendorMailboxes(id: string, emailAccountIds?: string[]): Promise<ListResponse<VendorReconnectMailbox>> {
    return await Request<ListResponse<VendorReconnectMailbox>>({
        method: "POST",
        url: `/emails/vendors/${id}/reconnect`,
        data: emailAccountIds?.length ? { email_account_ids: emailAccountIds } : {},
        authorization: true,
        timeout: 60_000,
    });
}

export async function getVendorReconnect(id: string): Promise<ListResponse<VendorReconnectMailbox>> {
    return await Request<ListResponse<VendorReconnectMailbox>>({ method: "GET", url: `/emails/vendors/${id}/reconnect`, authorization: true });
}
