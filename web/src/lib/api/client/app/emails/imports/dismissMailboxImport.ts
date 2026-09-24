import Request from "../../../Request";

// Hides an import from the recent list for the workspace, stopping it first when it is running.
export default async function dismissMailboxImport(id: string): Promise<void> {
    await Request<void>({
        method: "POST",
        url: `/emails/imports/${id}/dismiss`,
        authorization: true,
    });
}
