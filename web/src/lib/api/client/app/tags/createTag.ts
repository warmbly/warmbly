import Request from "../../Request";
import type Tag from "@/lib/api/models/app/Tag";

export default async function createTag(title: string, color?: string): Promise<Tag> {
    return await Request<Tag>({
        method: "POST",
        url: `/tags`,
        data: color ? { title, color } : { title },
        authorization: true,
    })
}
