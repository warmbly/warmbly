import type Team from "@/lib/api/models/app/teams/Team";
import Request from "../../Request";

export default async function addTeamMember(id: string, userId: string): Promise<Team> {
    return await Request<Team>({
        method: "POST",
        url: `/teams/${id}/members`,
        data: { user_id: userId },
        authorization: true,
    });
}
