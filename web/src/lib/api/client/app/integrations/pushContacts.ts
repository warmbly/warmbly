import Request from "../../Request";

export interface PushContactsInput {
    connectionId: string;
    contact_ids: string[];
}

export interface PushRecordResult {
    contact_id: string;
    email: string;
    ok: boolean;
    error?: string;
}

export interface PushResult {
    provider: string;
    pushed: number;
    failed: number;
    results: PushRecordResult[];
}

// Synchronously upserts the given org contacts into a connected CRM
// (HubSpot / Pipedrive / Salesforce / Close). Idempotent at the provider level
// (upsert by email), so retries are safe.
export default async function pushContacts(input: PushContactsInput): Promise<PushResult> {
    const { connectionId, ...body } = input;
    return await Request<PushResult>({
        method: "POST",
        url: `/integrations/connections/${connectionId}/push`,
        data: body,
        authorization: true,
    });
}
