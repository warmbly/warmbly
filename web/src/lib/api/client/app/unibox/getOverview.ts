import type UniboxOverview from "@/lib/api/models/app/unibox/UniboxOverview";
import Request from "../../Request";

export default async function getOverview(): Promise<UniboxOverview> {
    return await Request<UniboxOverview>({
        method: "GET",
        url: "/unibox/overview",
        authorization: true,
    })
}
