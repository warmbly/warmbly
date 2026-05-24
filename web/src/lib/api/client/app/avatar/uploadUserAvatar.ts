import Request from "../../Request";

// Avatar endpoints sidestep the JSON Request helper because the body
// is multipart/form-data. We use fetch directly but lean on Request's
// auth header logic via `Request` for parity.

export async function uploadUserAvatar(blob: Blob): Promise<{ avatar_url: string }> {
    const fd = new FormData();
    fd.append("file", blob, "avatar.jpg");
    return Request<{ avatar_url: string }>({
        method: "POST",
        url: "/me/avatar",
        data: fd,
        authorization: true,
    });
}

export async function deleteUserAvatar(): Promise<void> {
    return Request<void>({
        method: "DELETE",
        url: "/me/avatar",
        authorization: true,
    });
}

export async function uploadOrganizationAvatar(blob: Blob): Promise<{ avatar_url: string }> {
    const fd = new FormData();
    fd.append("file", blob, "avatar.jpg");
    return Request<{ avatar_url: string }>({
        method: "POST",
        url: "/organization/avatar",
        data: fd,
        authorization: true,
    });
}

export async function deleteOrganizationAvatar(): Promise<void> {
    return Request<void>({
        method: "DELETE",
        url: "/organization/avatar",
        authorization: true,
    });
}
