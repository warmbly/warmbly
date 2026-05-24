import type Role from "@/lib/api/models/app/admin/Role";
import Request from "../../../Request";

export default async function createRole(name: string): Promise<Role> {
    return await Request<Role>({
        method: "POST",
        url: "/roles",
        data: {
            name,
        },
        authorization: true,
    })
}
