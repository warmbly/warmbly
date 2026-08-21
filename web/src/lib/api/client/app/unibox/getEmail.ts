import type { UniboxEmailDetail } from "@/lib/api/models/app/unibox/UniboxEmail";
import Request from "../../Request";

export default async function getEmail(id: string): Promise<UniboxEmailDetail> {
    return await Request<UniboxEmailDetail>({
        method: "GET",
        url: `/unibox/${id}`,
        authorization: true,
    })
}
