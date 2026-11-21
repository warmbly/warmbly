// /v1/social: social accounts on every channel, the connect flow, the GraphKit link, conversations, voice note
// audio and LinkedIn lead search. Every write carries an Idempotency-Key; a reauth_required answer is retried by
// Request after the prompt.
import type {
    AiResult,
    ChannelSpec,
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
    LinkedInImport,
    LinkedInProfile,
    ParsedUrl,
    SavedSearch,
    SearchPage,
    SignIn,
    SocialAccount,
    SocialAccountPatch,
    SocialAudio,
    SocialChannel,
    SocialConnectQuote,
    SocialContactActivity,
    SocialConversation,
    SocialLinkStart,
    SocialList,
    SocialMessage,
    SocialStatus,
} from "@/lib/api/models/app/social/Social";
import Request from "../../Request";

const idem = (key?: string) => ({ "Idempotency-Key": key ?? crypto.randomUUID() });
const id = (v: string) => encodeURIComponent(v);

export async function getSocialStatus(signal?: AbortSignal): Promise<SocialStatus> {
    return await Request<SocialStatus>({ method: "GET", url: "/social/status", authorization: true, signal });
}

/** The channel catalog: actions, sign-in methods, limits and consent bases per channel. */
export async function getSocialChannels(signal?: AbortSignal): Promise<ChannelSpec[]> {
    const res = await Request<ChannelSpec[] | { channels?: ChannelSpec[]; data?: ChannelSpec[] }>({ method: "GET", url: "/social/channels", authorization: true, signal });
    return Array.isArray(res) ? res : (res?.channels ?? res?.data ?? []);
}

// ---------------------------------------------------------------- GraphKit link

export async function startSocialLink(): Promise<SocialLinkStart> {
    return await Request<SocialLinkStart>({ method: "POST", url: "/social/link", headers: idem(), authorization: true });
}

export async function completeSocialLink(session: string): Promise<SocialStatus> {
    return await Request<SocialStatus>({ method: "POST", url: `/social/link/${id(session)}/complete`, headers: idem(), authorization: true });
}

export async function unlinkSocial(): Promise<void> {
    await Request<void>({ method: "DELETE", url: "/social/link", headers: idem(), authorization: true });
}

// ---------------------------------------------------------------- connect sessions

export async function getConnectQuote(accountId: string | null, signal?: AbortSignal): Promise<SocialConnectQuote> {
    return await Request<SocialConnectQuote>({
        method: "GET",
        url: "/social/connect-quote",
        params: accountId ? { account_id: accountId } : undefined,
        authorization: true,
        signal,
    });
}

export async function createConnection(body: CreateConnection, idempotencyKey: string): Promise<{ session: ConnectSession; charged_micros: number }> {
    return await Request<{ session: ConnectSession; charged_micros: number }>({
        method: "POST",
        url: "/social/connections",
        data: body,
        headers: idem(idempotencyKey),
        authorization: true,
    });
}

export async function getConnection(sessionId: string, signal?: AbortSignal): Promise<ConnectSession> {
    return await Request<ConnectSession>({ method: "GET", url: `/social/connections/${id(sessionId)}`, authorization: true, signal });
}

export async function signInConnection(sessionId: string, body: SignIn): Promise<ConnectSession> {
    return await Request<ConnectSession>({ method: "POST", url: `/social/connections/${id(sessionId)}/sign-in`, data: body, headers: idem(), authorization: true });
}

export async function answerConnection(sessionId: string, body: CheckpointAnswer): Promise<ConnectSession> {
    return await Request<ConnectSession>({ method: "POST", url: `/social/connections/${id(sessionId)}/answer`, data: body, headers: idem(), authorization: true });
}

export async function chooseConnectionMethod(sessionId: string, challenge: string): Promise<ConnectSession> {
    return await Request<ConnectSession>({
        method: "POST",
        url: `/social/connections/${id(sessionId)}/method`,
        data: { challenge },
        headers: idem(),
        authorization: true,
    });
}

export async function resendConnectionCode(sessionId: string): Promise<ConnectSession> {
    return await Request<ConnectSession>({ method: "POST", url: `/social/connections/${id(sessionId)}/resend`, headers: idem(), authorization: true });
}

export async function cancelConnection(sessionId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/social/connections/${id(sessionId)}`, headers: idem(), authorization: true });
}

// ---------------------------------------------------------------- accounts

export async function listSocialAccounts(signal?: AbortSignal): Promise<SocialList<SocialAccount>> {
    return await Request<SocialList<SocialAccount>>({ method: "GET", url: "/social/accounts", params: { limit: 100 }, authorization: true, signal });
}

export async function getSocialAccount(accountId: string, signal?: AbortSignal): Promise<SocialAccount> {
    return await Request<SocialAccount>({ method: "GET", url: `/social/accounts/${id(accountId)}`, authorization: true, signal });
}

export async function patchSocialAccount(accountId: string, patch: SocialAccountPatch): Promise<SocialAccount> {
    return await Request<SocialAccount>({ method: "PATCH", url: `/social/accounts/${id(accountId)}`, data: patch, headers: idem(), authorization: true });
}

export type AccountAction = "pause" | "resume" | "resync";

export async function socialAccountAction(accountId: string, action: AccountAction): Promise<SocialAccount> {
    return await Request<SocialAccount>({ method: "POST", url: `/social/accounts/${id(accountId)}/${action}`, headers: idem(), authorization: true });
}

export async function removeSocialAccount(accountId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/social/accounts/${id(accountId)}`, headers: idem(), authorization: true });
}

// ---------------------------------------------------------------- voice note audio

/** Stores a recording for a voice note step (multipart `file`, audio/*). */
/** `durationSeconds` is measured in the browser before upload; the server refuses a file without it. */
export async function uploadSocialAudio(file: Blob, durationSeconds: number, filename = "voice-note"): Promise<SocialAudio> {
    const form = new FormData();
    form.append("file", file, filename);
    form.append("duration_seconds", String(durationSeconds));
    return await Request<SocialAudio>({ method: "POST", url: "/social/audio", data: form, headers: idem(), authorization: true });
}

export async function deleteSocialAudio(audioId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/social/audio/${id(audioId)}`, headers: idem(), authorization: true });
}

// ---------------------------------------------------------------- LinkedIn search

export async function getFinderCatalog(signal?: AbortSignal): Promise<FinderCatalog[]> {
    const res = await Request<FinderCatalog[] | { data: FinderCatalog[] }>({ method: "GET", url: "/social/search/catalog", authorization: true, signal });
    return Array.isArray(res) ? res : (res?.data ?? []);
}

export interface SearchRequest {
    account_id: string | null;
    query: FinderQuery;
    cursor: string | null;
    limit?: number;
}

export async function runFinderSearch(body: SearchRequest, signal?: AbortSignal): Promise<SearchPage> {
    return await Request<SearchPage>({ method: "POST", url: "/social/search", data: body, authorization: true, signal });
}

export async function finderTypeahead(
    p: { source: FinderSource; filter: string; q: string; account_id: string | null },
    signal?: AbortSignal,
): Promise<FinderOption[]> {
    const res = await Request<FinderOption[] | { data: FinderOption[] }>({
        method: "GET",
        url: "/social/search/typeahead",
        params: { source: p.source, filter: p.filter, q: p.q, account_id: p.account_id ?? undefined },
        authorization: true,
        signal,
    });
    return Array.isArray(res) ? res : (res?.data ?? []);
}

export async function parseFinderUrl(url: string): Promise<ParsedUrl> {
    return await Request<ParsedUrl>({ method: "POST", url: "/social/search/parse-url", data: { url }, authorization: true });
}

export async function finderAi(body: { prompt: string; source: FinderSource; current_query: FinderQuery | null; account_id?: string | null }): Promise<AiResult> {
    return await Request<AiResult>({ method: "POST", url: "/social/search/ai", data: body, headers: idem(), authorization: true });
}

/** A plain import answers its result; one that finds work emails answers 202 with the background import to follow. */
export async function importFinderHits(body: ImportRequest, idempotencyKey: string): Promise<ImportResult | ImportStarted> {
    return await Request<ImportResult | ImportStarted>({ method: "POST", url: "/social/search/import", data: body, headers: idem(idempotencyKey), authorization: true });
}

export async function getLinkedInImport(importId: string, signal?: AbortSignal): Promise<LinkedInImport> {
    return await Request<LinkedInImport>({ method: "GET", url: `/social/imports/${id(importId)}`, authorization: true, signal });
}

export async function listSavedSearches(signal?: AbortSignal): Promise<SavedSearch[]> {
    const res = await Request<SavedSearch[] | SocialList<SavedSearch>>({ method: "GET", url: "/social/saved-searches", authorization: true, signal });
    return Array.isArray(res) ? res : (res?.data ?? []);
}

export async function createSavedSearch(body: { name: string; query: FinderQuery }): Promise<SavedSearch> {
    return await Request<SavedSearch>({ method: "POST", url: "/social/saved-searches", data: body, headers: idem(), authorization: true });
}

export async function patchSavedSearch(searchId: string, body: { name?: string; query?: FinderQuery }): Promise<SavedSearch> {
    return await Request<SavedSearch>({ method: "PATCH", url: `/social/saved-searches/${id(searchId)}`, data: body, headers: idem(), authorization: true });
}

export async function deleteSavedSearch(searchId: string): Promise<void> {
    await Request<void>({ method: "DELETE", url: `/social/saved-searches/${id(searchId)}`, headers: idem(), authorization: true });
}

export async function getLinkedInProfile(identifier: string, accountId: string | null, signal?: AbortSignal): Promise<LinkedInProfile> {
    return await Request<LinkedInProfile>({
        method: "GET",
        url: "/social/profiles",
        params: { identifier, account_id: accountId ?? undefined },
        authorization: true,
        signal,
    });
}

// ---------------------------------------------------------------- conversations

export async function listSocialConversations(
    p: { cursor?: string | null; accountId?: string | null; contactId?: string | null; channel?: SocialChannel | null; unread?: boolean; limit?: number },
    signal?: AbortSignal,
): Promise<SocialList<SocialConversation>> {
    return await Request<SocialList<SocialConversation>>({
        method: "GET",
        url: "/social/conversations",
        params: {
            cursor: p.cursor ?? undefined,
            account_id: p.accountId ?? undefined,
            contact_id: p.contactId ?? undefined,
            channel: p.channel ?? undefined,
            unread: p.unread ? "true" : undefined,
            limit: p.limit ?? 50,
        },
        authorization: true,
        signal,
    });
}

export async function getSocialConversation(conversationId: string, signal?: AbortSignal): Promise<SocialConversation> {
    return await Request<SocialConversation>({ method: "GET", url: `/social/conversations/${id(conversationId)}`, authorization: true, signal });
}

export async function listSocialMessages(conversationId: string, cursor: string | null, signal?: AbortSignal): Promise<SocialList<SocialMessage>> {
    return await Request<SocialList<SocialMessage>>({
        method: "GET",
        url: `/social/conversations/${id(conversationId)}/messages`,
        params: { cursor: cursor ?? undefined, limit: 50 },
        authorization: true,
        signal,
    });
}

export async function sendSocialMessage(conversationId: string, body: string, idempotencyKey: string): Promise<SocialMessage> {
    return await Request<SocialMessage>({
        method: "POST",
        url: `/social/conversations/${id(conversationId)}/messages`,
        data: { body },
        headers: idem(idempotencyKey),
        authorization: true,
    });
}

export async function markSocialConversationRead(conversationId: string): Promise<void> {
    await Request<void>({ method: "POST", url: `/social/conversations/${id(conversationId)}/read`, headers: idem(), authorization: true });
}

export async function getSocialContactActivity(contactId: string, signal?: AbortSignal): Promise<SocialContactActivity> {
    const res = await Request<Partial<SocialContactActivity> | null>({ method: "GET", url: `/social/contacts/${id(contactId)}/activity`, authorization: true, signal });
    return { actions: res?.actions ?? [], conversations: res?.conversations ?? [] };
}

// ---------------------------------------------------------------- campaign senders

export async function getCampaignSocialAccounts(campaignId: string, signal?: AbortSignal): Promise<string[]> {
    const res = await Request<{ account_ids?: string[] | null } | null>({ method: "GET", url: `/campaigns/${id(campaignId)}/social-accounts`, authorization: true, signal });
    return res?.account_ids ?? [];
}

export async function putCampaignSocialAccounts(campaignId: string, accountIds: string[]): Promise<string[]> {
    const res = await Request<{ account_ids?: string[] | null } | null>({
        method: "PUT",
        url: `/campaigns/${id(campaignId)}/social-accounts`,
        data: { account_ids: accountIds },
        headers: idem(),
        authorization: true,
    });
    return res?.account_ids ?? accountIds;
}
