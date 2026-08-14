import { useQuery } from "@tanstack/react-query";
import getAuthConfig from "../../client/auth/getAuthConfig";
import type AuthConfig from "../../models/auth/AuthConfig";

/**
 * Deployment auth capabilities. Fetched once and cached for the session: it is
 * boot-time backend configuration, so it cannot change while the login screen
 * is open.
 *
 * The fallback matters as much as the fetch. If the endpoint is unreachable
 * (an older backend, or a proxy problem) the login screen must still render,
 * so it falls back to the hosted defaults.
 */
export const AUTH_CONFIG_FALLBACK: AuthConfig = {
    captcha: true,
    password_login: true,
    login_code: "always",
    registration: "false",
    email_verification: true,
    mail_delivers: true,
    passkeys: true,
    providers: [],
    self_hosted: false,
    setup_required: false,
};

export default function useAuthConfig() {
    const query = useQuery({
        queryKey: ["auth", "config"],
        queryFn: getAuthConfig,
        staleTime: Infinity,
        gcTime: Infinity,
        retry: 1,
    });

    return {
        ...query,
        config: query.data ?? AUTH_CONFIG_FALLBACK,
        // Callers must not render the captcha widget or provider buttons until
        // the real answer arrives, or the widget mounts and then unmounts.
        ready: !query.isLoading,
    };
}
