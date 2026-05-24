import type UnseenCount from "@/lib/api/models/app/unibox/UnseenCount";
import Request from "../../Request";

export default async function getUnseenCount(): Promise<UnseenCount> {
    return await Request<UnseenCount>({
        method: "GET",
        url: `/unibox/count`,
        authorization: true,
    })
}
