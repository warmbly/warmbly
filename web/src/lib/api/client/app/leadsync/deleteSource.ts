import Request from "../../Request";

// Deletes a saved source (org-scoped). Backend returns 204 No Content.
export default async function deleteSource(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/lead-sync/sources/${id}`,
        authorization: true,
    });
}
