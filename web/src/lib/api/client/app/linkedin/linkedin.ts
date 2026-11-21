// /v1/linkedin: LinkedIn accounts, the connect flow, the GraphKit link and lead search.
// Every write carries an Idempotency-Key; a reauth_required answer is retried by Request after the prompt.
import type {
    AiResult,
    CheckpointAnswer,
    ConnectSession,
    CreateConnection,
    FinderCatalog,
    FinderOption,
    FinderQuery,
    FinderSource,
    ImportRequest,
    ImportResult,
    ImportStarted,
    LinkedInAccount,
    LinkedInContactActivity,
    LinkedInConversation,
    LinkedInImport,
    LinkedInMessage,
    LinkedInAccountPatch,
    LinkedInConnectQuote,
    LinkedInLinkStart,
    LinkedInList,
    LinkedInProfile,
    LinkedInStatus,
    ParsedUrl,
    SavedSearch,
    SearchPage,
    SignIn,
} from "@/lib/api/models/app/linkedin/LinkedIn";
import Request from "../../Request";

const idem = (key?: string) => ({ "Idempotency-Key": key ?? crypto.randomUUID() });
const id = (v: string) => encodeURIComponent(v);

export async function getLinkedInStatus(signal?: AbortSignal): Promise<LinkedInStatus> {
    return await Request<LinkedInStatus>({ method: "GET", url: "/linkedin/status", authorization: true, signal });
}

// ---------------------------------------------------------------- GraphKit link

export async function startLinkedInLink(): Promise<LinkedInLinkStart> {
    return await Request<LinkedInLinkStart>({ method: "POST", url: "/linkedin/link", headers: idem(), authorization: true });
}

export async function completeLinkedInLink(session: string): Promise<LinkedInStatus> {
    return await Request<LinkedInStatus>({ method: "POST", url: `/linkedin/link/${id(session)}/complete`, headers: idem(), authorization: true });
}

export async function unlinkLinkedIn(): Promise<void> {
    await Request<void>({ method: "DELETE", url: "/linkedin/link", headers: idem(), authorization: true });
}

// ---------------------------------------------------------------- connect sessions

export async function getConnectQuote(accountId: string | null, signal?: AbortSignal): Promise<LinkedInConnectQuote> {
    return await Request<LinkedInConnectQuote>({
        method: "GET",
        url: "/linkedin/connect-quote",
        params: accountId ? { account_id: accountId } : undefined,
        authorization: true,
        signal,
    });
}

export async function createConnection(body: CreateConnection, idempotencyKey: string): Promise<{ session: ConnectSession; charged_micros: number }> {
    return await Request<{ session: ConnectSession; charged_micros: number }>({
        method: "POST",
        url: "/linkedin/connections",
        data: body,
        headers: idem(idempotencyKey),
        authorization: true,
    });
}

export async function getConnection(sessionId: string, signal?: AbortSignal): Promise<ConnectSession> {
    return await Request<ConnectSession>({ method: "GET", url: `/linkedin/connections/${id(sessionId)}`, authorization: true, signal });
}

export async function signInConnection(sessionId: string, body: SignIn): Promise<ConnectSession> {
    return await Request<ConnectSession>({ method: "POST", url: `/linkedin/connections/${id(sessionId)}/sign-in`, data: body, headers: idem(), authorization: true });
}

export async function answerConnection(sessionId: string, body: CheckpointAnswer): Promise<ConnectSession> {
    return await Request<ConnectSession>({ method: "POST", url: `/linkedin/connections/${id(sessionId)}/answer`, data: body, headers: idem(), authorization: true });
}

export async function chooseConnectionMethod(sessionId: string, challenge: string): Promise<ConnectSession> {
    return await Request<ConnectSession>({
        method: "POST",
        url: `/linkedin/connections/${id(sessionId)}/method`,
        data: { challenge },
        headers: idem(),
        authorization: true,
    });
}

export async function resendConnectionCode(sessionId: string): Promise<ConnectSession> {
    return await Request<ConnectSession>({ method: "POST", url: `/linkedin/connections/${id(sessionId)}/resend`, headers: idem(), authorization: true });
}

export async function cancelConnection(sessionId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/linkedin/connections/${id(sessionId)}`, headers: idem(), authorization: true });
}

// ---------------------------------------------------------------- accounts

export async function listLinkedInAccounts(signal?: AbortSignal): Promise<LinkedInList<LinkedInAccount>> {
    return await Request<LinkedInList<LinkedInAccount>>({ method: "GET", url: "/linkedin/accounts", params: { limit: 100 }, authorization: true, signal });
}

export async function getLinkedInAccount(accountId: string, signal?: AbortSignal): Promise<LinkedInAccount> {
    return await Request<LinkedInAccount>({ method: "GET", url: `/linkedin/accounts/${id(accountId)}`, authorization: true, signal });
}

export async function patchLinkedInAccount(accountId: string, patch: LinkedInAccountPatch): Promise<LinkedInAccount> {
    return await Request<LinkedInAccount>({ method: "PATCH", url: `/linkedin/accounts/${id(accountId)}`, data: patch, headers: idem(), authorization: true });
}

export type AccountAction = "pause" | "resume" | "resync";

export async function linkedInAccountAction(accountId: string, action: AccountAction): Promise<LinkedInAccount> {
    return await Request<LinkedInAccount>({ method: "POST", url: `/linkedin/accounts/${id(accountId)}/${action}`, headers: idem(), authorization: true });
}

export async function removeLinkedInAccount(accountId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/linkedin/accounts/${id(accountId)}`, headers: idem(), authorization: true });
}

// ---------------------------------------------------------------- search

export async function getFinderCatalog(signal?: AbortSignal): Promise<FinderCatalog[]> {
    const res = await Request<FinderCatalog[] | { data: FinderCatalog[] }>({ method: "GET", url: "/linkedin/search/catalog", authorization: true, signal });
    return Array.isArray(res) ? res : (res?.data ?? []);
}

export interface SearchRequest {
    account_id: string | null;
    query: FinderQuery;
    cursor: string | null;
    limit?: number;
}

export async function runFinderSearch(body: SearchRequest, signal?: AbortSignal): Promise<SearchPage> {
    return await Request<SearchPage>({ method: "POST", url: "/linkedin/search", data: body, authorization: true, signal });
}

export async function finderTypeahead(
    p: { source: FinderSource; filter: string; q: string; account_id: string | null },
    signal?: AbortSignal,
): Promise<FinderOption[]> {
    const res = await Request<FinderOption[] | { data: FinderOption[] }>({
        method: "GET",
        url: "/linkedin/search/typeahead",
        params: { source: p.source, filter: p.filter, q: p.q, account_id: p.account_id ?? undefined },
        authorization: true,
        signal,
    });
    return Array.isArray(res) ? res : (res?.data ?? []);
}

export async function parseFinderUrl(url: string): Promise<ParsedUrl> {
    return await Request<ParsedUrl>({ method: "POST", url: "/linkedin/search/parse-url", data: { url }, authorization: true });
}

export async function finderAi(body: { prompt: string; source: FinderSource; current_query: FinderQuery | null; account_id?: string | null }): Promise<AiResult> {
    return await Request<AiResult>({ method: "POST", url: "/linkedin/search/ai", data: body, headers: idem(), authorization: true });
}

/** A plain import answers its result; one that finds work emails answers 202 with the background import to follow. */
export async function importFinderHits(body: ImportRequest, idempotencyKey: string): Promise<ImportResult | ImportStarted> {
    return await Request<ImportResult | ImportStarted>({ method: "POST", url: "/linkedin/search/import", data: body, headers: idem(idempotencyKey), authorization: true });
}

export async function getLinkedInImport(importId: string, signal?: AbortSignal): Promise<LinkedInImport> {
    return await Request<LinkedInImport>({ method: "GET", url: `/linkedin/imports/${id(importId)}`, authorization: true, signal });
}

export async function listSavedSearches(signal?: AbortSignal): Promise<SavedSearch[]> {
    const res = await Request<SavedSearch[] | LinkedInList<SavedSearch>>({ method: "GET", url: "/linkedin/saved-searches", authorization: true, signal });
    return Array.isArray(res) ? res : (res?.data ?? []);
}

export async function createSavedSearch(body: { name: string; query: FinderQuery }): Promise<SavedSearch> {
    return await Request<SavedSearch>({ method: "POST", url: "/linkedin/saved-searches", data: body, headers: idem(), authorization: true });
}

export async function patchSavedSearch(searchId: string, body: { name?: string; query?: FinderQuery }): Promise<SavedSearch> {
    return await Request<SavedSearch>({ method: "PATCH", url: `/linkedin/saved-searches/${id(searchId)}`, data: body, headers: idem(), authorization: true });
}

export async function deleteSavedSearch(searchId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/linkedin/saved-searches/${id(searchId)}`, headers: idem(), authorization: true });
}

export async function getLinkedInProfile(identifier: string, accountId: string | null, signal?: AbortSignal): Promise<LinkedInProfile> {
    return await Request<LinkedInProfile>({
        method: "GET",
        url: "/linkedin/profiles",
        params: { identifier, account_id: accountId ?? undefined },
        authorization: true,
        signal,
    });
}

// ---------------------------------------------------------------- conversations

export async function listLinkedInConversations(
    p: { cursor?: string | null; accountId?: string | null; contactId?: string | null; unread?: boolean; limit?: number },
    signal?: AbortSignal,
): Promise<LinkedInList<LinkedInConversation>> {
    return await Request<LinkedInList<LinkedInConversation>>({
        method: "GET",
        url: "/linkedin/conversations",
        params: {
            cursor: p.cursor ?? undefined,
            account_id: p.accountId ?? undefined,
            contact_id: p.contactId ?? undefined,
            unread: p.unread ? "true" : undefined,
            limit: p.limit ?? 50,
        },
        authorization: true,
        signal,
    });
}

export async function getLinkedInConversation(conversationId: string, signal?: AbortSignal): Promise<LinkedInConversation> {
    return await Request<LinkedInConversation>({ method: "GET", url: `/linkedin/conversations/${id(conversationId)}`, authorization: true, signal });
}

export async function listLinkedInMessages(conversationId: string, cursor: string | null, signal?: AbortSignal): Promise<LinkedInList<LinkedInMessage>> {
    return await Request<LinkedInList<LinkedInMessage>>({
        method: "GET",
        url: `/linkedin/conversations/${id(conversationId)}/messages`,
        params: { cursor: cursor ?? undefined, limit: 50 },
        authorization: true,
        signal,
    });
}

export async function sendLinkedInMessage(conversationId: string, body: string, idempotencyKey: string): Promise<LinkedInMessage> {
    return await Request<LinkedInMessage>({
        method: "POST",
        url: `/linkedin/conversations/${id(conversationId)}/messages`,
        data: { body },
        headers: idem(idempotencyKey),
        authorization: true,
    });
}

export async function markLinkedInConversationRead(conversationId: string): Promise<void> {
    await Request<void>({ method: "POST", url: `/linkedin/conversations/${id(conversationId)}/read`, headers: idem(), authorization: true });
}

export async function getLinkedInContactActivity(contactId: string, signal?: AbortSignal): Promise<LinkedInContactActivity> {
    const res = await Request<Partial<LinkedInContactActivity> | null>({ method: "GET", url: `/linkedin/contacts/${id(contactId)}/activity`, authorization: true, signal });
    return { actions: res?.actions ?? [], conversations: res?.conversations ?? [] };
}

// ---------------------------------------------------------------- campaign senders

export async function getCampaignLinkedInAccounts(campaignId: string, signal?: AbortSignal): Promise<string[]> {
    const res = await Request<{ account_ids?: string[] | null } | null>({ method: "GET", url: `/campaigns/${id(campaignId)}/linkedin-accounts`, authorization: true, signal });
    return res?.account_ids ?? [];
}

export async function putCampaignLinkedInAccounts(campaignId: string, accountIds: string[]): Promise<string[]> {
    const res = await Request<{ account_ids?: string[] | null } | null>({
        method: "PUT",
        url: `/campaigns/${id(campaignId)}/linkedin-accounts`,
        data: { account_ids: accountIds },
        headers: idem(),
        authorization: true,
    });
    return res?.account_ids ?? accountIds;
}
