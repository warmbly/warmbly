// Select / create organization gate.
//
// Shown when a logged-in user has no current organization. Three
// affordances on one page:
//
//   1. Pending invitations — accept-to-join (one-click).
//   2. Existing memberships — pick one to enter the dashboard.
//   3. Create new — name + slate-900 button, mints an org and drops
//      the user straight into it.
//
// The router redirects here from /app whenever organizations.length
// is 0 and there's no currentOrganization set.

import React from "react";
import { useNavigate } from "react-router-dom";
import { Loader2Icon, MailIcon, PlusIcon, UsersIcon } from "lucide-react";
import toast from "react-hot-toast";
import useOrganizations from "@/lib/api/hooks/app/organizations/useOrganizations";
import useMyInvitations from "@/lib/api/hooks/app/organizations/useMyInvitations";
import useCreateOrganization from "@/lib/api/hooks/app/organizations/useCreateOrganization";
import useAcceptInvitation from "@/lib/api/hooks/app/organizations/useAcceptInvitation";
import useSwitchOrganization from "@/lib/api/hooks/app/organizations/useSwitchOrganization";
import { useAppStore } from "@/stores";
import { Logo } from "@/components/svg";
import { Label, TextInput } from "@/components/ui/field";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function SelectOrgPage() {
    const navigate = useNavigate();
    const newOrgRequested =
        typeof window !== "undefined" &&
        new URLSearchParams(window.location.search).get("new") === "1";

    const orgs = useOrganizations();
    const invites = useMyInvitations();
    const setOrganizations = useAppStore((s) => s.setOrganizations);
    const setCurrentOrganization = useAppStore((s) => s.setCurrentOrganization);

    const create = useCreateOrganization();
    const accept = useAcceptInvitation();
    const switchOrg = useSwitchOrganization();

    const orgList = orgs.data ?? [];
    const inviteList = invites.data ?? [];

    // Cache invalidation in the hooks already refreshes the query, but
    // we also seed the appStore so the rest of the dashboard sees the
    // org immediately on navigation.
    React.useEffect(() => {
        if (orgList.length > 0) setOrganizations(orgList);
    }, [orgList, setOrganizations]);

    const [name, setName] = React.useState("");

    async function onCreate() {
        const t = name.trim();
        if (t.length < 2) {
            toast.error("Name is required");
            return;
        }
        try {
            const org = await toast.promise(create.mutateAsync({ name: t }), {
                loading: "Creating workspace…",
                success: "Workspace created",
                error: (e: AppError) => buildError(e),
            });
            // Activate the newly created org and enter the dashboard.
            await switchOrg.mutateAsync(org.id).catch(() => undefined);
            setCurrentOrganization(org);
            navigate("/app/emails", { replace: true });
        } catch {
            /* surfaced */
        }
    }

    async function onAccept(invitationId: string) {
        try {
            await toast.promise(accept.mutateAsync({ invitation_id: invitationId }), {
                loading: "Joining workspace…",
                success: "Joined",
                error: (e: AppError) => buildError(e),
            });
            // Re-load orgs after acceptance and route into the first one.
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
                    <h1 className="text-[18px] font-semibold text-slate-900 mb-1">
                        Pick a workspace to continue
                    </h1>
                    <p className="text-[12.5px] text-slate-500 mb-5 leading-relaxed">
                        Workspaces hold your campaigns, mailboxes and team. Create one to start,
                        or join an existing one if you were invited.
                    </p>

                    {loading && (
                        <div className="h-24 flex items-center justify-center">
                            <Loader2Icon className="w-4 h-4 animate-spin text-slate-400" />
                        </div>
                    )}

                    {/* Pending invitations */}
                    {!loading && inviteList.length > 0 && (
                        <div className="mb-5">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                                <MailIcon className="w-3 h-3" />
                                Invitations ({inviteList.length})
                            </div>
                            <div className="border border-slate-200 rounded-md overflow-hidden divide-y divide-slate-200/60">
                                {inviteList.map((inv) => (
                                    <div
                                        key={inv.id}
                                        className="px-3 py-2.5 flex items-center gap-2.5"
                                    >
                                        <div className="size-7 rounded bg-slate-100 text-slate-700 flex items-center justify-center text-[10px] font-semibold shrink-0">
                                            {(inv.organization_name ?? "?").slice(0, 2).toUpperCase()}
                                        </div>
                                        <div className="min-w-0 flex-1">
                                            <div className="text-[12.5px] font-medium text-slate-900 truncate">
                                                {inv.organization_name ?? "Workspace"}
                                            </div>
                                            <div className="text-[11px] text-slate-500 truncate">
                                                Pending invitation · {inv.role}
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

                    {/* Existing memberships */}
                    {!loading && orgList.length > 0 && (
                        <div className="mb-5">
                            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                                <UsersIcon className="w-3 h-3" />
                                Your workspaces ({orgList.length})
                            </div>
                            <div className="border border-slate-200 rounded-md overflow-hidden divide-y divide-slate-200/60">
                                {orgList.map((o) => (
                                    <button
                                        key={o.id}
                                        type="button"
                                        onClick={() => onPickExisting(o.id)}
                                        disabled={switchOrg.isPending}
                                        className="w-full px-3 py-2.5 flex items-center gap-2.5 hover:bg-slate-50/80 transition-colors text-left disabled:opacity-50"
                                    >
                                        <div className="size-7 rounded bg-slate-900 text-white flex items-center justify-center text-[10px] font-semibold shrink-0">
                                            {o.name.slice(0, 2).toUpperCase()}
                                        </div>
                                        <div className="min-w-0 flex-1">
                                            <div className="text-[12.5px] font-medium text-slate-900 truncate">
                                                {o.name}
                                            </div>
                                            <div className="text-[11px] text-slate-500 truncate font-mono">
                                                {o.id.slice(0, 8)}
                                            </div>
                                        </div>
                                        <span className="text-[11px] text-slate-400">Open →</span>
                                    </button>
                                ))}
                            </div>
                        </div>
                    )}

                    {/* Create new */}
                    <div className="border border-slate-200 rounded-md overflow-hidden">
                        <div className="h-9 px-3 border-b border-slate-200 flex items-center gap-1.5">
                            <PlusIcon className="w-3 h-3 text-slate-400" />
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                {newOrgRequested || orgList.length === 0 ? "Create your first workspace" : "Create another workspace"}
                            </span>
                        </div>
                        <div className="px-3 py-3 space-y-2">
                            <div>
                                <Label>Name</Label>
                                <TextInput
                                    value={name}
                                    onChange={setName}
                                    placeholder="Acme outbound"
                                    autoFocus
                                    className="w-full"
                                    onKeyDown={(e) => {
                                        if (e.key === "Enter") onCreate();
                                    }}
                                />
                            </div>
                            <p className="text-[11px] text-slate-400">
                                You'll be the owner. You can rename it later.
                            </p>
                            <div className="pt-1">
                                <button
                                    type="button"
                                    onClick={onCreate}
                                    disabled={create.isPending || name.trim().length < 2}
                                    className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                                >
                                    {create.isPending ? (
                                        <Loader2Icon className="w-3 h-3 animate-spin" />
                                    ) : (
                                        <PlusIcon className="w-3 h-3" />
                                    )}
                                    Create workspace
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}
