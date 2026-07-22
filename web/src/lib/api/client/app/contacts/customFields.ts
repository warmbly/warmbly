import Request from "../../Request";

// GET /contacts/custom-fields — the org's distinct contact custom-field keys,
// frequency-ranked, so a personalization picker can suggest real fields instead
// of making the user type them blind. Returns a bare string[].
export default async function getCustomFieldKeys(): Promise<string[]> {
    const body = await Request<{ data: string[] }>({
        method: "GET",
        url: "/contacts/custom-fields",
        authorization: true,
    });
    return body?.data ?? [];
}
