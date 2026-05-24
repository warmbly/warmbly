import type UniboxEmail from "@/lib/api/models/app/unibox/UniboxEmail";
import Request from "../../Request";

export default async function getEmail(id: string): Promise<UniboxEmail> {
    return await Request<UniboxEmail>({
        method: "GET",
        url: `/unibox/${id}`,
        authorization: true,
    })
}
