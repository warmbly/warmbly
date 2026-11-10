import { AgencyPage } from "@/components/app/agency/AgencyPage";
import React from "react";
import { ChevronDownIcon, Loader2Icon, MailIcon, MoreHorizontalIcon, UserPlusIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { EmptyBlock, PageBody, SectionBar, TopbarAction } from "@/components/layout/Page";
import { Label, TextInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { useAppStore } from "@/stores";
import {
    useAddAgencyStaff,
    useAgencyStaff,
    useCancelAgencyInvitation,
    useCurrentAgency,
    useRemoveAgencyStaff,
    useUpdateAgencyStaff,
} from "@/lib/api/hooks/app/agency/useAgency";
import type { AgencyMember, AgencyRole } from "@/lib/api/models/app/agency/Agency";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { AgencyDialog } from "@/components/app/agency/AgencyDialog";
import { ghostBtn, primaryBtn } from "@/components/app/agency/styles";

const ROLE_LABEL: Record<AgencyRole, string> = { owner: "Owner", admin: "Admin", member: "Member" };
const ROLE_HINT: Record<AgencyRole, string> = {
    owner: "Admin in every client workspace, and the agency's billing.",
    admin: "Admin in every client workspace. Manages clients, staff and billing.",
    member: "Manager in every client workspace: campaigns, contacts, mailboxes.",
};

function AddStaffDialog({ open, onClose, agencyId, isOwner }: { open: boolean; onClose: () => void; agencyId: string; isOwner: boolean }) {
    const [email, setEmail] = React.useState("");
    const [role, setRole] = React.useState<AgencyRole>("member");
    const add = useAddAgencyStaff(agencyId);
    React.useEffect(() => {
        if (!open) {
            setEmail("");
            setRole("member");
        }
    }, [open]);
    const valid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim());

    async function submit() {
        if (!valid) return;
        try {
            const res = await add.mutateAsync({ email: email.trim(), role });
            toast.success(res.invitation ? "Invitation saved. They join when they sign in with that address." : "Added to the agency");
            onClose();
        } catch (e) {
            toast.error(buildError(e as unknown as AppError));
        }
    }

    const roles: AgencyRole[] = isOwner ? ["member", "admin"] : ["member"];
    return (
        <AgencyDialog
            open={open}
            onClose={onClose}
            eyebrow="Add"
            title="Staff member"
            icon={<UserPlusIcon className="w-3 h-3" />}
            footer={
                <>
                    <button type="button" onClick={onClose} className={cn("ml-auto", ghostBtn)}>
                        Cancel
                    </button>
                    <button type="button" onClick={() => void submit()} disabled={add.isPending || !valid} className={primaryBtn}>
                        {add.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Add
                    </button>
                </>
            }
        >
            <form
                className="px-4 py-4 space-y-3"
                onSubmit={(e) => {
                    e.preventDefault();
                    void submit();
                }}
            >
                <div>
                    <Label>Email</Label>
                    <TextInput value={email} onChange={setEmail} type="email" placeholder="name@agency.com" autoFocus className="w-full" />
                </div>
                <div>
                    <Label>Role</Label>
                    <div className="space-y-1">
                        {roles.map((r) => (
                            <button
                                key={r}
                                type="button"
                                role="radio"
                                aria-checked={role === r}
                                onClick={() => setRole(r)}
                                className={cn(
                                    "w-full px-2.5 py-2 rounded-md border text-left transition-colors",
                                    role === r ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:border-slate-300",
                                )}
                            >
                                <div className="text-[12.5px] font-medium text-slate-900">{ROLE_LABEL[r]}</div>
                                <div className="text-[11px] text-slate-500">{ROLE_HINT[r]}</div>
                            </button>
                        ))}
                    </div>
                </div>
                <p className="text-[11px] text-slate-400 leading-relaxed">
                    Someone without a Warmbly account joins the agency the first time they sign in with this address.
                </p>
            </form>
        </AgencyDialog>
    );
}

function AgencyStaff() {
    const { agency } = useCurrentAgency();
    const agencyId = agency?.id ?? "";
    const staff = useAgencyStaff(agency?.id);
    const update = useUpdateAgencyStaff(agencyId);
    const remove = useRemoveAgencyStaff(agencyId);
    const cancel = useCancelAgencyInvitation(agencyId);
    const confirm = useConfirm();
    const me = useAppStore((s) => s.user?.id);
    const [adding, setAdding] = React.useState(false);
    const role = agency?.role;
    const canManage = role === "owner" || role === "admin";

    if (!agency) return null;
    const members = staff.data?.data ?? [];
    const invites = staff.data?.invitations ?? [];

    const canChange = (m: AgencyMember) => m.role !== "owner" && (role === "owner" || (role === "admin" && m.role === "member"));

    const askRemove = (m: AgencyMember) =>
        confirm.show(
            m.user_id === me
                ? "Leave this agency? You lose access to every client workspace it gave you."
                : `Remove ${m.email} from the agency? They lose access to every client workspace the agency gave them.`,
            async () => {
                try {
                    await remove.mutateAsync(m.user_id);
                } catch (e) {
                    toast.error(buildError(e as unknown as AppError));
                }
            },
            undefined,
            { title: m.user_id === me ? "Leave agency" : "Remove staff member", confirmLabel: m.user_id === me ? "Leave" : "Remove" },
        );

    return (
        <PageBody>
            <SectionBar label="Staff" count={members.length}>
                {canManage && (
                    <TopbarAction icon={<UserPlusIcon className="w-3 h-3" />} onClick={() => setAdding(true)}>
                        Add staff
                    </TopbarAction>
                )}
            </SectionBar>
            <p className="px-5 py-2.5 text-[11.5px] text-slate-500 border-b border-slate-200/60">
                Staff are added to every client workspace: owners and admins as Admin, members as Manager. Removing someone takes
                back only the access the agency gave.
            </p>
            {members.map((m) => {
                const name = [m.first_name, m.last_name].filter(Boolean).join(" ") || m.email;
                return (
                    <div key={m.user_id} className="group min-h-11 px-5 py-1.5 flex items-center gap-3 border-b border-slate-200/60">
                        <div className="flex-1 min-w-0">
                            <div className="text-[12.5px] text-slate-900 font-medium truncate">{name}</div>
                            <div className="text-[11px] text-slate-400 truncate">{m.email}</div>
                        </div>
                        {canChange(m) && role === "owner" ? (
                            <PopoverMenu align="end">
                                <PopoverMenuTrigger asChild>
                                    <button type="button" className="h-7 px-2 rounded-md border border-slate-200 text-[12px] text-slate-700 inline-flex items-center gap-1">
                                        {ROLE_LABEL[m.role]}
                                        <ChevronDownIcon className="w-3 h-3 text-slate-400" />
                                    </button>
                                </PopoverMenuTrigger>
                                <PopoverMenuContent minWidth={240}>
                                    {(["admin", "member"] as const).map((r) => (
                                        <PopoverMenuItem
                                            key={r}
                                            selected={m.role === r}
                                            description={ROLE_HINT[r]}
                                            onSelect={() =>
                                                update.mutate({ userId: m.user_id, role: r }, { onError: (e) => toast.error(buildError(e as unknown as AppError)) })
                                            }
                                        >
                                            {ROLE_LABEL[r]}
                                        </PopoverMenuItem>
                                    ))}
                                </PopoverMenuContent>
                            </PopoverMenu>
                        ) : (
                            <span className="text-[12px] text-slate-500">{ROLE_LABEL[m.role]}</span>
                        )}
                        {(canChange(m) || (m.user_id === me && m.role !== "owner")) && (
                            <PopoverMenu align="end">
                                <PopoverMenuTrigger asChild>
                                    <button
                                        type="button"
                                        aria-label={`Actions for ${m.email}`}
                                        className="size-7 rounded-md text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus:opacity-100"
                                    >
                                        <MoreHorizontalIcon className="w-3.5 h-3.5" />
                                    </button>
                                </PopoverMenuTrigger>
                                <PopoverMenuContent minWidth={180}>
                                    <PopoverMenuItem danger onSelect={() => askRemove(m)}>
                                        {m.user_id === me ? "Leave agency" : "Remove from agency"}
                                    </PopoverMenuItem>
                                </PopoverMenuContent>
                            </PopoverMenu>
                        )}
                    </div>
                );
            })}
            {invites.length > 0 && (
                <>
                    <SectionBar label="Waiting to sign in" count={invites.length} />
                    {invites.map((inv) => (
                        <div key={inv.id} className="min-h-11 px-5 py-1.5 flex items-center gap-3 border-b border-slate-200/60">
                            <MailIcon className="w-3.5 h-3.5 text-slate-400" />
                            <span className="flex-1 min-w-0 text-[12.5px] text-slate-700 truncate">{inv.email}</span>
                            <span className="text-[12px] text-slate-500">{ROLE_LABEL[inv.role]}</span>
                            {canManage && (
                                <button
                                    type="button"
                                    aria-label={`Cancel the invitation for ${inv.email}`}
                                    onClick={() => cancel.mutate(inv.id, { onError: (e) => toast.error(buildError(e as unknown as AppError)) })}
                                    className="size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center"
                                >
                                    <XIcon className="w-3.5 h-3.5" />
                                </button>
                            )}
                        </div>
                    ))}
                </>
            )}
            {!staff.isLoading && members.length === 0 && <EmptyBlock title="No staff yet" />}
            <AddStaffDialog open={adding} onClose={() => setAdding(false)} agencyId={agencyId} isOwner={role === "owner"} />
        </PageBody>
    );
}

export default function AgencyStaffPage() {
    return (
        <AgencyPage title="Staff">
            <AgencyStaff />
        </AgencyPage>
    );
}
