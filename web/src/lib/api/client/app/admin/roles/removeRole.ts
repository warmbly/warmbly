import type Role from "@/lib/api/models/app/admin/Role";
import Request from "../../../Request";

export default async function removeRole(userId: string, roleId: string): Promise<Role> {
    return await Request<Role>({
        method: "DELETE",
        url: `/users/${userId}/roles/${roleId}`,
        authorization: true,
    })
}

