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
 * and it must fail safe rather than hosted-safe: never offer a capability we
 * could not confirm, and never present open signup on what may well be an
 * unclaimed instance behind a broken proxy.
 */
export const AUTH_CONFIG_FALLBACK: AuthConfig = {
    // A token we cannot reach is a token the server cannot verify.
    captcha: false,
    password_login: true,
    login_code: "off",
    registration: "invite_only",
    invites_required: true,
    email_verification: false,
    mail_delivers: false,
    passkeys: false,
    providers: [],
    self_hosted: true,
    // The app-password walkthrough works on every deployment; Google sign-in
    // for a new Gmail mailbox is only offered on a confirmed yes.
    gmail_oauth_connect: false,
    // Unconfirmed billing keeps the subscription-based gates in place rather
    // than presenting an unreachable backend as an unlocked one.
    billing_enabled: true,
    setup_required: false,
    docs_url: "https://docs.warmbly.com/development/accounts-and-access/",
    // An unreachable backend cannot tell us whose instance this is, and the
    // fallback fails self-host-safe everywhere else, so it claims no branding
    // either: no website, no terms, no privacy link.
    brand: { name: "Cloudsnow" },
};

export default function useAuthConfig() {
    const query = useQuery({
        queryKey: ["auth", "config"],
        queryFn: getAuthConfig,
        staleTime: Infinity,
        gcTime: Infinity,
        retry: 1,
    });

    const data = query.data;
    const config: AuthConfig = data
        ? {
              ...AUTH_CONFIG_FALLBACK,
              ...data,
              providers: Array.isArray(data.providers) ? data.providers : [],
          }
        : AUTH_CONFIG_FALLBACK;

    return {
        ...query,
        config,
        // Callers must not render the captcha widget or provider buttons until
        // the real answer arrives, or the widget mounts and then unmounts.
        ready: !query.isLoading,
        // The screen is running on the fallback, so it should say so instead of
        // presenting guesses as this deployment's real capabilities.
        unreachable: query.isError,
    };
}
