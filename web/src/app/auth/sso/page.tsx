// SSO landing (/auth/sso?code=...).
//
// The identity provider redirects the browser to the backend callback, which
// completes the exchange server-side and sends the user here with a single-use
// code. This page trades that code for the session and drops the user into the
// dashboard, so a token never appears in a URL or in history.

import { useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2Icon, AlertCircleIcon } from "lucide-react";

import exchangeSSO from "@/lib/api/client/auth/exchangeSSO";
import getUser from "@/lib/api/client/auth/getUser";
import { saveTokens } from "@/lib/auth";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";

export default function SSOCallbackPage() {
    const [params] = useSearchParams();
    const navigate = useNavigate();
    const queryClient = useQueryClient();
    const [error, setError] = useState("");
    // The code is single use, so React 18's double-invoked effect in dev would
    // burn it on the first run and fail on the second.
    const started = useRef(false);

    useEffect(() => {
        if (started.current) return;
        started.current = true;

        const code = params.get("code");
        if (!code) {
            setError("This sign-in link is missing its code.");
            return;
        }

        (async () => {
            try {
                const session = await exchangeSSO(code);
                saveTokens(session as unknown as Record<string, unknown>);
                queryClient.clear();
                try {
                    await queryClient.fetchQuery({ queryKey: ["auth", "me"], queryFn: getUser });
                } catch {
                    // UserProvider retries and redirects on a genuine failure.
                }
                navigate("/app/emails", { replace: true });
            } catch (err) {
                setError(buildError(err as AppError));
            }
        })();
    }, [params, navigate, queryClient]);

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
                        <button
                            onClick={() => navigate("/auth/login", { replace: true })}
                            className="mt-5 w-full h-10 rounded-md bg-slate-900 text-white text-[13px] font-medium hover:bg-slate-800 transition-colors"
                        >
                            Back to sign in
                        </button>
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
