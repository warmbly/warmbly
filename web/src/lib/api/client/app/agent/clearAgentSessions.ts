import Request from "../../Request";

// Deletes the member's entire assistant history in the current workspace.
export default async function clearAgentSessions(): Promise<{ deleted: number }> {
    return await Request<{ message: string; deleted: number }>({
        method: "DELETE",
        url: `/ai/sessions`,
        authorization: true,
    });
}
