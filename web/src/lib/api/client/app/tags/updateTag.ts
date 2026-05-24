import Request from "../../Request";
import type Tag from "@/lib/api/models/app/Tag";

export default async function updateTag(id: string, tag: Partial<Tag>): Promise<Tag> {
    return await Request<Tag>({
        method: "PATCH",
        url: `/tags/${id}`,
        data: tag,
        authorization: true,
    })
}
