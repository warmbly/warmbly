import type Role from "@/lib/api/models/app/admin/Role";
import Request from "../../../Request";

export default async function updateRole(id: string, role: Partial<Role>): Promise<Role> {
    return await Request<Role>({
        method: "PATCH",
        url: `/roles/${id}`,
        data: role,
        authorization: true,
    })
}
