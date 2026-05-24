import type Access from "../../../../models/app/admin/Access";
import Request from "../../../Request";

export default async function getRoles(): Promise<Access> {
    return await Request<Access>({
        method: "GET",
        url: "/roles"
    })
}
