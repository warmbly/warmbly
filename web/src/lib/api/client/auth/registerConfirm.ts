import type RegisterConfirm from "@/lib/api/models/auth/RegisterConfirm";
import Request from "../Request";

export default async function registerConfirm(data: RegisterConfirm): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: "/auth/register/confirm",
        data,
    })
}
