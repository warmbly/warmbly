import Request from "../../Request";

export default async function revokeOtherSessions(): Promise<void> {
    await Request<void>({
        method: "DELETE",
        url: "/auth/sessions",
        authorization: true,
    });
}
