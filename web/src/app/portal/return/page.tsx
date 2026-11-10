// /portal/return#code= on an agency portal: trades the one-time code the main
// address issued for a session on this portal, then opens the dashboard.

import { useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2Icon } from "lucide-react";

import { exchangePortalHandoff } from "@/lib/api/client/auth/portalHandoff";
import getUser from "@/lib/api/client/auth/getUser";
import { saveTokens } from "@/lib/auth";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";
import { currentPortal, handoffCode, mainSignInURL } from "@/lib/portal";
import { PortalCard, PortalError } from "../PortalCard";

export default function PortalReturnPage() {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const portal = currentPortal();
    const [error, setError] = useState("");
    // The code is single use, so a second run of the effect must not spend it.
    const started = useRef(false);

    useEffect(() => {
        if (started.current) return;
        started.current = true;
        const code = handoffCode(window.location.hash);
        // The code leaves the address bar and history before anything else happens.
        window.history.replaceState(window.history.state, "", window.location.pathname);
        if (!portal.portal || !code) {
            setError("This sign-in link is missing its code.");
            return;
        }
        (async () => {
            try {
                const token = await exchangePortalHandoff(code);
                saveTokens(token as unknown as Record<string, unknown>);
                queryClient.clear();
                try {
                    await queryClient.fetchQuery({ queryKey: ["auth", "me"], queryFn: getUser });
                } catch {
                    // UserProvider retries and redirects on a genuine failure.
                }
                navigate({ to: "/app/emails", replace: true });
            } catch (err) {
                setError(buildError(err as AppError));
            }
        })();
    }, [navigate, queryClient, portal]);

    const retry = mainSignInURL(portal, typeof window === "undefined" ? "" : window.location.host);

    return (
        <PortalCard brand={portal.portal ? portal.brand : undefined} title={error ? "Sign-in failed" : "Signing you in"}>
            {error ? (
                <PortalError
                    message={error}
                    action={
                        retry ? (
                            <a
                                href={retry}
                                className="mt-5 w-full h-10 rounded-md bg-sky-600 text-white text-[13px] font-medium hover:bg-sky-700 transition-colors inline-flex items-center justify-center"
                            >
                                Sign in again
                            </a>
                        ) : null
                    }
                />
            ) : (
                <Loader2Icon className="w-5 h-5 animate-spin text-slate-400 mx-auto mt-4" />
            )}
        </PortalCard>
    );
}
