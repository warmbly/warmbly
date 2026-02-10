import Request from "../../Request";

export default async function deleteContactNote(contactId: string, noteId: string): Promise<void> {
    return await Request<void>({
        method: "DELETE",
        url: `/contacts/${contactId}/notes/${noteId}`,
        authorization: true,
    })
}
