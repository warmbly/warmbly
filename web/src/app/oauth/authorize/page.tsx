// OAuth 2.1 consent screen. The third-party app redirects the user's browser
// here with the standard authorize params; we validate them via the API, show
// the app + the scopes it wants, and on approval mint a code and bounce the
// browser back to the app's redirect URI.

"use client";

import React from "react";
import { useSearchParams } from "react-router-dom";
import { CheckIcon, ExternalLinkIcon, ShieldCheckIcon } from "lucide-react";

import getAuthorizeDetails from "@/lib/api/client/app/oauth/getAuthorizeDetails";
import authorizeConsent from "@/lib/api/client/app/oauth/authorizeConsent";
import type { OAuthConsentInfo } from "@/lib/api/models/app/oauth/OAuthApp";

function errText(e: unknown, fallback: string): string {
    const o = e as { error_description?: string; message?: string };
    return o?.error_description ?? o?.message ?? fallback;
}

// Executable/local pseudo-schemes must never be navigated to: the server already
// rejects them at client registration, this is the defense-in-depth backstop so a
// redirect can never run script in the dashboard origin.
const UNSAFE_REDIRECT_SCHEMES = new Set([
    "javascript:",
    "data:",
    "vbscript:",
    "file:",
    "blob:",
    "about:",
    "filesystem:",
]);

function isSafeRedirect(rawUrl: string): boolean {
    try {
        return !UNSAFE_REDIRECT_SCHEMES.has(new URL(rawUrl).protocol.toLowerCase());
    } catch {
        return false;
    }
}

export default function OAuthConsentPage() {
    const [sp] = useSearchParams();
    const params = React.useMemo(() => Object.fromEntries(sp.entries()), [sp]);
    const [info, setInfo] = React.useState<OAuthConsentInfo | null>(null);
    const [error, setError] = React.useState<string | null>(null);
    const [loading, setLoading] = React.useState(true);
    const [submitting, setSubmitting] = React.useState(false);

    React.useEffect(() => {
        let cancelled = false;
        getAuthorizeDetails(params)
            .then((i) => {
                if (!cancelled) {
                    setInfo(i);
                    setLoading(false);
                }
            })
            .catch((e) => {
                if (!cancelled) {
                    setError(errText(e, "This authorization request is invalid."));
                    setLoading(false);
                }
            });
        return () => {
            cancelled = true;
        };
    }, [params]);

    const approve = async () => {
        if (!info) return;
        setSubmitting(true);
        try {
            const { redirect_url } = await authorizeConsent({
                response_type: params.response_type ?? "code",
                client_id: params.client_id ?? "",
                redirect_uri: params.redirect_uri ?? "",
                scope: params.scope ?? "",
                state: params.state ?? "",
                code_challenge: params.code_challenge ?? "",
                code_challenge_method: params.code_challenge_method ?? "",
            });
            if (!isSafeRedirect(redirect_url)) {
                setError("This app's redirect URL is not allowed.");
                setSubmitting(false);
                return;
            }
            window.location.href = redirect_url;
        } catch (e) {
            setError(errText(e, "Could not complete authorization."));
            setSubmitting(false);
        }
    };

    const deny = () => {
        if (info?.redirect_uri && isSafeRedirect(info.redirect_uri)) {
            const u = new URL(info.redirect_uri);
            u.searchParams.set("error", "access_denied");
            if (info.state) u.searchParams.set("state", info.state);
            window.location.href = u.toString();
        } else {
            window.history.back();
        }
    };

    const card = "w-full max-w-md rounded-2xl bg-white shadow-xl ring-1 ring-slate-200 overflow-hidden";

    if (loading) {
        return (
            <div className={card}>
                <div className="p-8 text-center text-[13px] text-slate-400">Checking the request…</div>
            </div>
        );
    }

    if (error || !info) {
        return (
            <div className={card}>
                <div className="p-6 text-center">
                    <div className="mx-auto mb-3 flex h-10 w-10 items-center justify-center rounded-full bg-rose-50 text-rose-600">
                        <ShieldCheckIcon className="h-5 w-5" />
                    </div>
                    <p className="text-[14px] font-semibold text-slate-800">This request can't be completed</p>
                    <p className="mt-1 text-[12.5px] text-slate-500">{error ?? "Invalid authorization request."}</p>
                </div>
            </div>
        );
    }

    return (
        <div className={card}>
            <div className="flex flex-col items-center gap-2 border-b border-slate-100 px-6 pt-7 pb-5">
                {info.logo_url ? (
                    <img src={info.logo_url} alt={info.name} className="h-12 w-12 rounded-xl object-cover ring-1 ring-slate-200" />
                ) : (
                    <div className="flex h-12 w-12 items-center justify-center rounded-xl bg-sky-50 text-[18px] font-semibold uppercase text-sky-700 ring-1 ring-sky-100">
                        {info.name.charAt(0)}
                    </div>
                )}
                <h1 className="text-[15px] font-semibold text-slate-900">{info.name}</h1>
                <p className="text-center text-[12.5px] text-slate-500">
                    wants to access your Warmbly workspace
                </p>
                {info.website_url && (
                    <a
                        href={info.website_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 text-[11.5px] text-sky-700 hover:underline"
                    >
                        <ExternalLinkIcon className="h-3 w-3" />
                        {info.website_url.replace(/^https?:\/\//, "")}
                    </a>
                )}
            </div>

            <div className="px-6 py-5">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-2">It will be able to</div>
                <ul className="space-y-1.5">
                    {info.scopes.map((s) => (
                        <li key={s} className="flex items-start gap-2 text-[12.5px] text-slate-700">
                            <CheckIcon className="mt-0.5 h-3.5 w-3.5 shrink-0 text-emerald-500" />
                            <span className="capitalize">{s.replace(/_/g, " ")}</span>
                        </li>
                    ))}
                </ul>
            </div>

            <div className="flex items-center gap-2 border-t border-slate-100 px-6 py-4">
                <button
                    onClick={deny}
                    disabled={submitting}
                    className="h-9 flex-1 rounded-lg border border-slate-200 text-[13px] font-medium text-slate-600 hover:bg-slate-50 disabled:opacity-60"
                >
                    Deny
                </button>
                <button
                    onClick={approve}
                    disabled={submitting}
                    className="h-9 flex-1 rounded-lg bg-sky-600 text-[13px] font-medium text-white hover:bg-sky-700 disabled:opacity-60"
                >
                    {submitting ? "Authorizing…" : "Authorize"}
                </button>
            </div>
            <p className="px-6 pb-5 text-center text-[11px] text-slate-400 leading-relaxed">
                You'll be redirected to {(() => {
                    try {
                        return new URL(info.redirect_uri).host;
                    } catch {
                        return "the app";
                    }
                })()}. Only authorize apps you trust.
            </p>
        </div>
    );
}
