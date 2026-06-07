import Request from "../../Request";

export default async function removeTeamMember(id: string, userId: string): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: `/teams/${id}/members/${userId}`,
        authorization: true,
    });
}
