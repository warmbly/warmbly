import Request from "../../Request";

// Deletes a conversation and its transcript (sessions are private to the
// member, so this needs no org permission beyond membership).
export default async function deleteAgentSession(id: string): Promise<void> {
    await Request<{ message: string }>({
        method: "DELETE",
        url: `/ai/sessions/${id}`,
        authorization: true,
    });
}
