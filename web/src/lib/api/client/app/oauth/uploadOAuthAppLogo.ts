import Request from "../../Request";

// Uploads an OAuth app logo (multipart) and returns its public URL. Called during
// the registration wizard's branding step, before the app exists; the URL is then
// sent in the create payload as logo_url.
export default async function uploadOAuthAppLogo(blob: Blob): Promise<{ logo_url: string }> {
    const fd = new FormData();
    fd.append("file", blob, "logo.png");
    return await Request<{ logo_url: string }>({
        method: "POST",
        url: "/oauth/application-logo",
        data: fd,
        authorization: true,
    });
}
