import Request from "../Request";

// device_tokens names the remembered-device tokens this browser holds, so the server forgets them too.
export default async function logout(deviceTokens: string[] = []): Promise<void> {
    await Request<void>({
        method: "POST",
        url: "/auth/logout",
        authorization: true,
        data: deviceTokens.length ? { device_tokens: deviceTokens } : undefined,
    });
}
