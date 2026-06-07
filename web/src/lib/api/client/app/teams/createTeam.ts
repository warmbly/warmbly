import type Team from "@/lib/api/models/app/teams/Team";
import Request from "../../Request";

export default async function createTeam(data: { name: string; color?: string }): Promise<Team> {
    return await Request<Team>({
        method: "POST",
        url: "/teams",
        data,
        authorization: true,
    });
}
