import Request from "../Request";

export default async function changePassword(data: { current_password: string; new_password: string }): Promise<void> {
    return await Request<void>({
        method: "POST",
        url: "/me/password",
        authorization: true,
        data,
    })
}
