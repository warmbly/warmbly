import type { APIPermissionsResponse } from "@/lib/api/models/app/apikeys/APIPermission";
import Request from "../../Request";

export default async function listAPIPermissions(): Promise<APIPermissionsResponse> {
    return await Request<APIPermissionsResponse>({
        method: "GET",
        url: `/api-keys/permissions`,
        authorization: true,
    });
}
