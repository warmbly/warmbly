import type Team from "@/lib/api/models/app/teams/Team";
import Request from "../../Request";

export default async function listTeams(): Promise<Team[]> {
    const res = await Request<{ data: Team[] } | Team[]>({
        method: "GET",
        url: "/teams",
        authorization: true,
    });
    if (Array.isArray(res)) return res;
    return res?.data ?? [];
}
