// Select / create / manage organizations.
//
// Three responsibilities on one screen:
//
//   1. Pending invitations — accept-to-join.
//   2. Existing memberships — pick one to enter; each row shows
//      role + plan + age so the list is actually informative, not
//      a "click random row" gamble.
//   3. Create new — single slate-900 button that opens the same
//      NewWorkspaceDialog the OrgSwitcher uses. No inline form
//      anymore (was redundant with the dialog and made the two
//      entry points behave differently).
//
// The router redirects here from /app whenever the user has no
// current organization, so this page must handle both "first
// landing, no orgs" and "ongoing management" cases.

import React from "react";
import { useNavigate } from "react-router-dom";
import { Loader2Icon, MailIcon, PlusIcon, UsersIcon } from "lucide-react";
import toast from "react-hot-toast";
import useOrganizations from "@/lib/api/hooks/app/organizations/useOrganizations";
import useMyInvitations from "@/lib/api/hooks/app/organizations/useMyInvitations";
import useAcceptInvitation from "@/lib/api/hooks/app/organizations/useAcceptInvitation";
import useSwitchOrganization from "@/lib/api/hooks/app/organizations/useSwitchOrganization";
import { useAppStore } from "@/stores";
import { Logo } from "@/components/svg";
import { NewWorkspaceDialog } from "@/components/app/organizations/NewWorkspaceDialog";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

function relativeAge(d: Date | string): string {
    const date = typeof d === "string" ? new Date(d) : d;
    const diff = Date.now() - date.getTime();
    const days = Math.floor(diff / 86_400_000);
    if (days < 1) return "today";
    if (days < 7) return `${days}d ago`;
    if (days < 30) return `${Math.floor(days / 7)}w ago`;
    if (days < 365) return `${Math.floor(days / 30)}mo ago`;
    return `${Math.floor(days / 365)}y ago`;
}

function initials(name: string): string {
    return name
        .split(" ")
        .filter(Boolean)
        .map((w) => w[0])
        .join("")
        .toUpperCase()
        .slice(0, 2);
}

export default function SelectOrgPage() {
    const navigate = useNavigate();
    const [createOpen, setCreateOpen] = React.useState(false);

    const orgs = useOrganizations();
    const invites = useMyInvitations();
    const setOrganizations = useAppStore((s) => s.setOrganizations);
    const setCurrentOrganization = useAppStore((s) => s.setCurrentOrganization);
    const currentOrg = useAppStore((s) => s.currentOrganization);

    const accept = useAcceptInvitation();
    const switchOrg = useSwitchOrganization();

    const orgList = orgs.data ?? [];
    const inviteList = invites.data ?? [];

    React.useEffect(() => {
        if (orgList.length > 0) setOrganizations(orgList);
    }, [orgList, setOrganizations]);

    async function onAccept(invitationId: string) {
        try {
            await toast.promise(accept.mutateAsync({ invitation_id: invitationId }), {
                loading: "Joining workspace…",
                success: "Joined",
                error: (e: AppError) => buildError(e),
            });
            const fresh = await orgs.refetch();
            const list = fresh.data ?? [];
            if (list.length > 0) {
                await switchOrg.mutateAsync(list[0].id).catch(() => undefined);
                setOrganizations(list);
                setCurrentOrganization(list[0]);
                navigate("/app/emails", { replace: true });
            }
        } catch {
            /* surfaced */
        }
    }

    async function onPickExisting(orgId: string) {
        try {
            await switchOrg.mutateAsync(orgId);
            const org = orgList.find((o) => o.id === orgId);
            if (org) setCurrentOrganization(org);
            navigate("/app/emails", { replace: true });
        } catch (e) {
            toast.error(buildError(e as AppError));
        }
    }

    const loading = orgs.isPending || invites.isPending;
    const noWorkspaces = !loading && orgList.length === 0 && inviteList.length === 0;

    return (
        <div className="min-h-screen bg-[#f5f6f8] flex items-center justify-center px-4 py-12">
            <div className="w-full max-w-[480px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.08)] overflow-hidden">
                <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                    <Logo className="w-4 text-slate-900" />
                    <span
                        style={{ fontFamily: "var(--font-display)" }}
                        className="font-bold text-[13px] tracking-tight text-slate-900"
                    >
                        Warmbly
                    </span>
                    <div className="h-4 w-px bg-slate-200" />
                    <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        Workspaces
                    </span>
                </div>

                <div className="px-5 py-5">
                    {loading && (
                        <div className="h-24 flex items-center justify-center">
                            <Loader2Icon className="w-4 h-4 animate-spin text-slate-400" />
                        </div>
                    )}

                    {!loading && noWorkspaces && (
                        <EmptyFirstRun onCreate={() => setCreateOpen(true)} />
                    )}

                    {!loading && !noWorkspaces && (
                        <>
                            <h1 className="text-[16px] font-semibold text-slate-900 mb-1">
                                Pick a workspace
                            </h1>
                            <p className="text-[12px] text-slate-500 mb-5 leading-relaxed">
                                Workspaces hold your campaigns, mailboxes and team.
                            </p>

                            {/* Pending invitations */}
                            {inviteList.length > 0 && (
                                <div className="mb-5">
                                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                                        <MailIcon className="w-3 h-3" />
                                        Invitations
                                        <span className="font-mono tabular-nums">
                                            {inviteList.length}
                                        </span>
                                    </div>
                                    <div className="border border-slate-200 rounded-md overflow-hidden divide-y divide-slate-200/60">
                                        {inviteList.map((inv) => (
                                            <div
                                                key={inv.id}
                                                className="px-3 py-2.5 flex items-center gap-2.5"
                                            >
                                                <div className="size-7 rounded bg-slate-100 text-slate-700 flex items-center justify-center text-[10px] font-semibold shrink-0">
                                                    {initials(inv.organization_name ?? "?")}
                                                </div>
                                                <div className="min-w-0 flex-1">
                                                    <div className="text-[12.5px] font-medium text-slate-900 truncate">
                                                        {inv.organization_name ?? "Workspace"}
                                                    </div>
                                                    <div className="text-[11px] text-slate-500 truncate">
                                                        Pending · {inv.role}
                                                    </div>
                                                </div>
                                                <button
                                                    type="button"
                                                    onClick={() => onAccept(inv.id)}
                                                    disabled={accept.isPending}
                                                    className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors disabled:opacity-60 shrink-0"
                                                >
                                                    Join
                                                </button>
                                            </div>
                                        ))}
                                    </div>
                                </div>
                            )}

                            {/* Existing memberships — informative rows. Role +
                                plan + how long ago you joined replace the
                                opaque "id slice + Open →" filler. */}
                            {orgList.length > 0 && (
                                <div className="mb-5">
                                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                                        <UsersIcon className="w-3 h-3" />
                                        Your workspaces
                                        <span className="font-mono tabular-nums">{orgList.length}</span>
                                    </div>
                                    <div className="border border-slate-200 rounded-md overflow-hidden divide-y divide-slate-200/60">
                                        {orgList.map((o) => {
                                            const isCurrent = currentOrg?.id === o.id;
                                            return (
                                                <button
                                                    key={o.id}
                                                    type="button"
                                                    onClick={() => onPickExisting(o.id)}
                                                    disabled={switchOrg.isPending}
                                                    className={`w-full px-3 py-2.5 flex items-center gap-2.5 transition-colors text-left disabled:opacity-50 ${
                                                        isCurrent
                                                            ? "bg-sky-50/60 hover:bg-sky-50"
                                                            : "hover:bg-slate-50/80"
                                                    }`}
                                                >
                                                    <div className="size-7 rounded bg-slate-900 text-white flex items-center justify-center text-[10px] font-semibold shrink-0">
                                                        {initials(o.name)}
                                                    </div>
                                                    <div className="min-w-0 flex-1">
                                                        <div className="flex items-center gap-1.5">
                                                            <span className="text-[12.5px] font-medium text-slate-900 truncate">
                                                                {o.name}
                                                            </span>
                                                            {isCurrent && (
                                                                <span className="text-[9.5px] uppercase tracking-[0.1em] text-sky-700 bg-sky-100 px-1 rounded-sm font-semibold">
                                                                    Current
                                                                </span>
                                                            )}
                                                        </div>
                                                        <div className="text-[11px] text-slate-500 truncate flex items-center gap-1.5">
                                                            <span className="uppercase tracking-[0.08em]">
                                                                {o.role}
                                                            </span>
                                                            {o.plan && (
                                                                <>
                                                                    <span className="text-slate-300">·</span>
                                                                    <span>{o.plan}</span>
                                                                </>
                                                            )}
                                                            {o.created_at && (
                                                                <>
                                                                    <span className="text-slate-300">·</span>
                                                                    <span className="font-mono tabular-nums text-slate-400">
                                                                        joined {relativeAge(o.created_at)}
                                                                    </span>
                                                                </>
                                                            )}
                                                        </div>
                                                    </div>
                                                    <span className="text-[11px] text-slate-400 shrink-0">
                                                        {isCurrent ? "Resume →" : "Open →"}
                                                    </span>
                                                </button>
                                            );
                                        })}
                                    </div>
                                </div>
                            )}

                            {/* Create new — single action button. Same dialog
                                the OrgSwitcher uses; no inline form anymore. */}
                            <button
                                type="button"
                                onClick={() => setCreateOpen(true)}
                                className="w-full h-9 rounded-md border border-dashed border-slate-300 hover:border-slate-400 text-slate-700 hover:text-slate-900 inline-flex items-center justify-center gap-1.5 text-[12.5px] font-medium transition-colors"
                            >
                                <PlusIcon className="w-3.5 h-3.5" />
                                New workspace
                            </button>
                        </>
                    )}
                </div>
            </div>

            <NewWorkspaceDialog open={createOpen} onClose={() => setCreateOpen(false)} />
        </div>
    );
}

// Cleaner empty state for first-time users: single CTA, no list
// scaffolding for empty arrays, and a hint about invites.
function EmptyFirstRun({ onCreate }: { onCreate: () => void }) {
    return (
        <div className="text-center py-3">
            <div className="size-10 rounded-md bg-slate-100 text-slate-600 inline-flex items-center justify-center mb-3">
                <UsersIcon className="w-4 h-4" />
            </div>
            <h1 className="text-[16px] font-semibold text-slate-900 mb-1">
                Create your first workspace
            </h1>
            <p className="text-[12px] text-slate-500 mb-5 leading-relaxed max-w-[34ch] mx-auto">
                Workspaces hold your campaigns, mailboxes and team. You'll be the
                owner and can invite teammates anytime.
            </p>
            <button
                type="button"
                onClick={onCreate}
                className="h-8 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12.5px] font-medium inline-flex items-center gap-1.5 transition-colors"
            >
                <PlusIcon className="w-3.5 h-3.5" />
                New workspace
            </button>
            <p className="text-[11px] text-slate-400 mt-4">
                Already invited? The invitation will appear here once it's sent.
            </p>
        </div>
    );
}
