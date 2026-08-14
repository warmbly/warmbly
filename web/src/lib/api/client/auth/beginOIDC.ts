import Request from "../Request";

/**
 * Starts a generic OpenID Connect sign-in. The backend returns the
 * authorization URL rather than redirecting, because the dashboard is a
 * single-page app on a different origin from the API.
 */
export default async function beginOIDC(): Promise<{ url: string }> {
    return await Request<{ url: string }>({
        method: "POST",
        url: "/auth/oidc/begin",
    });
}
