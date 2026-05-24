import Request from "../Request";

export default async function logout(): Promise<void> {
    await Request<void>({
        method: "POST",
        url: "/auth/logout",
        authorization: true,
    });
}
