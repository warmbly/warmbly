import Request from "../../Request";

export default async function deleteTeam(id: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/teams/${id}`,
        authorization: true,
    });
}
