// /auth/partner?code=: a reseller that manages this workspace signs a person in with a one-time link.
// The code leaves the address bar before anything else, is spent once, and the dashboard opens.

import { useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { AlertCircleIcon, Loader2Icon } from "lucide-react";

import exchangePartnerLogin from "@/lib/api/client/auth/exchangePartnerLogin";
import getUser from "@/lib/api/client/auth/getUser";
import { saveTokens } from "@/lib/auth";
import buildError from "@/lib/helper/buildError";
import { hrefTarget } from "@/lib/routerSearch";
import type { AppError } from "@/lib/api/client/normalizeError";

/** An in-app path the server already vetted; anything else lands on the dashboard. */
function inAppPath(raw: string): string | null {
    if (!raw.startsWith("/") || raw.startsWith("//") || raw.includes("\\")) return null;
    return raw;
}

export default function PartnerSignInPage() {
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const [error, setError] = useState("");
    // The code is single use, so a second run of the effect must not spend it.
    const started = useRef(false);

    useEffect(() => {
        if (started.current) return;
        started.current = true;
        const code = new URLSearchParams(window.location.search).get("code");
        window.history.replaceState(window.history.state, "", window.location.pathname);
        if (!code) {
            setError("This sign-in link is missing its code.");
            return;
        }
        (async () => {
            try {
                const session = await exchangePartnerLogin(code);
                saveTokens(session as unknown as Record<string, unknown>);
                queryClient.clear();
                try {
                    await queryClient.fetchQuery({ queryKey: ["auth", "me"], queryFn: getUser });
                } catch {
                    // UserProvider retries and redirects on a genuine failure.
                }
                if (session.return_to && /^https?:\/\//i.test(session.return_to)) {
                    window.location.replace(session.return_to);
                    return;
                }
                const path = inAppPath(session.return_to ?? "");
                if (path) {
                    navigate({ ...hrefTarget(path), replace: true });
                } else {
                    navigate({ to: "/app/emails", replace: true });
                }
            } catch (err) {
                setError(buildError(err as AppError));
            }
        })();
    }, [navigate, queryClient]);

    return (
        <div className="min-h-screen grid place-items-center bg-slate-50 px-4">
            <div className="w-full max-w-sm bg-white rounded-xl border border-slate-200 p-6 shadow-sm text-center">
                {error ? (
                    <>
                        <div className="mx-auto w-12 h-12 rounded-xl bg-red-50 grid place-items-center mb-4">
                            <AlertCircleIcon className="w-6 h-6 text-red-500" />
                        </div>
                        <h1 className="text-[18px] font-semibold text-slate-900">Sign-in failed</h1>
                        <p className="text-sm text-slate-500 mt-2">{error}</p>
                        <p className="text-[12px] text-slate-400 mt-3">
                            Go back to the service that manages your workspace and open it from there again.
                        </p>
                    </>
                ) : (
                    <>
                        <Loader2Icon className="w-5 h-5 animate-spin text-slate-400 mx-auto" />
                        <p className="text-sm text-slate-500 mt-3">Signing you in…</p>
                    </>
                )}
            </div>
        </div>
    );
}
