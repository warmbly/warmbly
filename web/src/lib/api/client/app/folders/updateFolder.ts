import type Folder from "@/lib/api/models/app/Folder";
import Request from "../../Request";

export default async function updateFolder(id: string, folder: Partial<Folder>): Promise<Folder> {
    return await Request<Folder>({
        method: "PATCH",
        url: `/folders/${id}`,
        data: folder,
        authorization: true,
    })
}
