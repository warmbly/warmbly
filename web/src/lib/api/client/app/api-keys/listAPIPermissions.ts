import type APIPermission from "@/lib/api/models/app/apikeys/APIPermission";
import Request from "../../Request";

export default async function listAPIPermissions(): Promise<APIPermission[]> {
    return await Request<APIPermission[]>({
        method: "GET",
        url: `/api-keys/permissions`,
        authorization: true,
    })
}
