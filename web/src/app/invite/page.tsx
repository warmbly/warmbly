// Public invitation landing page (/invite?token=...).
//
// The token is the capability: anyone holding it can see who invited them
// where (a safe preview, no permissions/ids), then accept. Works for both
// brand-new users (sign up, bounce back, accept) and logged-in users.

import React from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { Loader2Icon, AlertCircleIcon, MailIcon } from "lucide-react";
import toast from "react-hot-toast";
import getToken from "@/lib/helper/getToken";
import usePreviewInvitation from "@/lib/api/hooks/app/organizations/usePreviewInvitation";
import useAcceptInvitation from "@/lib/api/hooks/app/organizations/useAcceptInvitation";
import useOrganizations from "@/lib/api/hooks/app/organizations/useOrganizations";
import useSwitchOrganization from "@/lib/api/hooks/app/organizations/useSwitchOrganization";
import useAuthConfig from "@/lib/api/hooks/auth/useAuthConfig";
import { useAppStore } from "@/stores";
import { Logo } from "@/components/svg";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function InviteAcceptPage() {
    const [params] = useSearchParams();
    const token = params.get("token");
    const navigate = useNavigate();
    const loggedIn = !!getToken();

    const preview = usePreviewInvitation(token);
    const accept = useAcceptInvitation();
    const orgs = useOrganizations();
    const switchOrg = useSwitchOrganization();
    const setOrganizations = useAppStore((s) => s.setOrganizations);
    const setCurrentOrganization = useAppStore((s) => s.setCurrentOrganization);
    const { config: authConfig } = useAuthConfig();

    const nextPath = `/invite?token=${encodeURIComponent(token ?? "")}`;
    // The invited address has to travel too: the backend only accepts a signup
    // whose email equals the invited one, so the form is prefilled with it.
    const invitedEmail = preview.data?.email ?? "";
    // The token has to travel as ?invite= too: without it the backend cannot
    // tell the signup is invited, and an invite_only instance refuses it.
    const registerPath =
        `/auth/register?invite=${encodeURIComponent(token ?? "")}` +
        (invitedEmail ? `&email=${encodeURIComponent(invitedEmail)}` : "") +
        `&next=${encodeURIComponent(nextPath)}`;
    const signupClosed = authConfig.registration === "true";

    async function onAccept() {
        if (!token) return;
        try {
            await toast.promise(accept.mutateAsync({ token }), {
                loading: "Joining workspace…",
                success: "Joined",
                error: (e: AppError) => buildError(e),
            });
            const fresh = await orgs.refetch();
            const list = fresh.data ?? [];
            const joined = list.find((o) => o.name === preview.data?.organization_name) ?? list[0];
            if (joined) {
                await switchOrg.mutateAsync(joined.id).catch(() => undefined);
                setOrganizations(list);
                setCurrentOrganization(joined);
            }
            navigate("/app/emails", { replace: true });
        } catch {
            /* surfaced */
        }
    }

    return (
        <div className="min-h-screen flex items-center justify-center bg-slate-50 px-4">
            <div className="w-full max-w-sm">
                <div className="flex justify-center mb-6">
                    <Logo className="h-7 w-auto" />
                </div>

                <div className="rounded-lg border border-slate-200 bg-white p-6 shadow-sm">
                    {!token ? (
                        <Centered icon={<AlertCircleIcon className="w-5 h-5 text-rose-500" />} title="Invalid link">
                            This invitation link is missing its token. Ask whoever invited you for a fresh link.
                        </Centered>
                    ) : preview.isPending ? (
                        <Centered icon={<Loader2Icon className="w-5 h-5 text-slate-400 animate-spin" />} title="Loading invitation…" />
                    ) : preview.isError || !preview.data ? (
                        <Centered icon={<AlertCircleIcon className="w-5 h-5 text-rose-500" />} title="Invitation not found">
                            This invitation is invalid or has been revoked.
                        </Centered>
                    ) : preview.data.expired ? (
                        <Centered icon={<AlertCircleIcon className="w-5 h-5 text-amber-500" />} title="Invitation expired">
                            This invitation has expired. Ask for a new one.
                        </Centered>
                    ) : (
                        <div>
                            <div className="flex items-center gap-3 mb-4">
                                <div className="size-10 rounded-lg bg-sky-50 border border-sky-100 text-sky-700 flex items-center justify-center text-[14px] font-semibold overflow-hidden shrink-0">
                                    {preview.data.organization_avatar ? (
                                        <img src={preview.data.organization_avatar} alt="" className="w-full h-full object-cover" />
                                    ) : (
                                        preview.data.organization_name.slice(0, 2).toUpperCase()
                                    )}
                                </div>
                                <div className="min-w-0">
                                    <div className="text-[14px] font-semibold text-slate-900 truncate">
                                        {preview.data.organization_name}
                                    </div>
                                    <div className="text-[12px] text-slate-500 truncate">
                                        {preview.data.inviter_name
                                            ? `${preview.data.inviter_name} invited you`
                                            : "You've been invited"}
                                    </div>
                                </div>
                            </div>

                            <div className="rounded-md bg-slate-50 border border-slate-200/70 px-3 py-2.5 mb-4 space-y-1.5">
                                <Row label="Invited email" value={preview.data.email} />
                                {preview.data.roles.length > 0 && (
                                    <div className="flex items-start gap-2 text-[12px]">
                                        <span className="text-slate-400 w-20 shrink-0">Role{preview.data.roles.length > 1 ? "s" : ""}</span>
                                        <span className="flex flex-wrap gap-1">
                                            {preview.data.roles.map((r) => (
                                                <span
                                                    key={r.id}
                                                    className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[10px] font-medium border"
                                                    style={{ backgroundColor: `${r.color || "#64748b"}14`, borderColor: `${r.color || "#64748b"}55`, color: r.color || "#475569" }}
                                                >
                                                    <span className="size-1.5 rounded-full" style={{ backgroundColor: r.color || "#64748b" }} />
                                                    {r.name}
                                                </span>
                                            ))}
                                        </span>
                                    </div>
                                )}
                            </div>

                            {loggedIn ? (
                                <button
                                    type="button"
                                    onClick={onAccept}
                                    disabled={accept.isPending}
                                    className="w-full h-9 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13px] font-medium inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-60"
                                >
                                    {accept.isPending && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                                    Accept invitation
                                </button>
                            ) : (
                                <div className="space-y-2">
                                    <p className="text-[12px] text-slate-500 flex items-center gap-1.5">
                                        <MailIcon className="w-3.5 h-3.5 shrink-0" />
                                        Sign in or create an account with <span className="font-medium text-slate-700">{preview.data.email}</span> to join.
                                    </p>
                                    <Link
                                        to={`/auth/login?next=${encodeURIComponent(nextPath)}`}
                                        className="w-full h-9 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[13px] font-medium inline-flex items-center justify-center transition-colors"
                                    >
                                        Sign in to accept
                                    </Link>
                                    {signupClosed ? (
                                        <p className="text-[12px] text-slate-500 leading-relaxed">
                                            This server is not accepting new accounts. Ask the administrator who
                                            invited you.
                                        </p>
                                    ) : (
                                        <Link
                                            to={registerPath}
                                            className="w-full h-9 rounded-md border border-slate-200 bg-white hover:bg-slate-50 text-slate-700 text-[13px] font-medium inline-flex items-center justify-center transition-colors"
                                        >
                                            Create an account
                                        </Link>
                                    )}
                                </div>
                            )}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}

function Centered({ icon, title, children }: { icon: React.ReactNode; title: string; children?: React.ReactNode }) {
    return (
        <div className="text-center py-4">
            <div className="flex justify-center mb-2">{icon}</div>
            <div className="text-[13px] font-semibold text-slate-900">{title}</div>
            {children && <p className="text-[12px] text-slate-500 leading-relaxed mt-1">{children}</p>}
        </div>
    );
}

function Row({ label, value }: { label: string; value: string }) {
    return (
        <div className="flex items-center gap-2 text-[12px]">
            <span className="text-slate-400 w-20 shrink-0">{label}</span>
            <span className="text-slate-700 font-medium truncate">{value}</span>
        </div>
    );
}
