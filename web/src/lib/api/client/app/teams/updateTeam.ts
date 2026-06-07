import type Team from "@/lib/api/models/app/teams/Team";
import Request from "../../Request";

export default async function updateTeam(
    id: string,
    data: { name?: string; color?: string },
): Promise<Team> {
    return await Request<Team>({
        method: "PATCH",
        url: `/teams/${id}`,
        data,
        authorization: true,
    });
}
