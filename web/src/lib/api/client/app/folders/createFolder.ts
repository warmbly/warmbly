import type Folder from "@/lib/api/models/app/Folder";
import Request from "../../Request";

export default async function createFolder(title: string, color?: string): Promise<Folder> {
    return await Request<Folder>({
        method: "POST",
        url: `/folders`,
        data: color ? { title, color } : { title },
        authorization: true,
    })
}
