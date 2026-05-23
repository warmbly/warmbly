import React from "react";
import { CheckIcon, Loader2Icon, MailIcon, PlusIcon, TrashIcon, UsersIcon, XIcon } from "lucide-react";
import { AnimatePresence, motion } from "framer-motion";
import toast from "react-hot-toast";

import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    TopbarAction,
} from "@/components/layout/Page";
import { Label, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuItem,
    PopoverMenuLabel,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import useMembers from "@/lib/api/hooks/app/organizations/useMembers";
import useInviteMember from "@/lib/api/hooks/app/organizations/useInviteMember";
import usePendingInvitations from "@/lib/api/hooks/app/organizations/usePendingInvitations";
import useCancelInvitation from "@/lib/api/hooks/app/organizations/useCancelInvitation";
import useRemoveMember from "@/lib/api/hooks/app/organizations/useRemoveMember";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const ROLES: Array<{ id: "admin" | "member"; label: string; description: string }> = [
    { id: "admin", label: "Admin", description: "Manage team, billing and settings" },
    { id: "member", label: "Member", description: "Send campaigns, manage mail" },
];

export default function TeamPage() {
    const confirm = useConfirm();
    const [open, setOpen] = React.useState(false);

    const members = useMembers();
    const invitations = usePendingInvitations();
    const cancelInvite = useCancelInvitation();
    const removeMember = useRemoveMember();

    const memberList = members.data ?? [];
    const inviteList = invitations.data ?? [];

    const remove = (id: string, name: string) => {
        confirm?.show(`Remove ${name}?`, async () => {
            try {
                await toast.promise(removeMember.mutateAsync(id), {
                    loading: "Removing…",
                    success: "Member removed",
                    error: (e: AppError) => buildError(e),
                });
            } catch {
                /* surfaced */
            }
        });
    };

    const cancel = (id: string) => {
        confirm?.show(`Cancel this invitation?`, async () => {
            try {
                await toast.promise(cancelInvite.mutateAsync(id), {
                    loading: "Cancelling…",
                    success: "Invitation cancelled",
                    error: (e: AppError) => buildError(e),
                });
            } catch {
                /* surfaced */
            }
        });
    };

    return (
        <Page>
            <PageTopbar
                eyebrow="Team"
                subtitle={`${memberList.length} member${memberList.length === 1 ? "" : "s"}${inviteList.length > 0 ? ` · ${inviteList.length} pending` : ""}`}
            >
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => setOpen(true)}
                >
                    Invite member
                </TopbarAction>
            </PageTopbar>

            <SectionBar label="Members" count={memberList.length} />
            <div className="border-b border-slate-200/60">
                {members.isPending ? (
                    <SkeletonRows />
                ) : memberList.length === 0 ? (
                    <EmptyBlock
                        title="No team members yet"
                        body="Invite teammates to collaborate on campaigns, mailboxes, and reporting."
                        cta={
                            <TopbarAction
                                icon={<PlusIcon className="w-3 h-3" />}
                                onClick={() => setOpen(true)}
                            >
                                Invite member
                            </TopbarAction>
                        }
                    />
                ) : (
                    <div className="divide-y divide-slate-200/60">
                        {memberList.map((m) => (
                            <div
                                key={m.user_id}
                                className="group h-11 px-5 flex items-center gap-3 hover:bg-slate-50/80 transition-colors"
                            >
                                <div className="size-6 rounded-full bg-slate-100 flex items-center justify-center shrink-0">
                                    <span className="text-[9.5px] font-semibold text-slate-600">
                                        {(m.email ?? m.user_id).slice(0, 2).toUpperCase()}
                                    </span>
                                </div>
                                <div className="min-w-0 flex-1">
                                    <div className="text-[12.5px] text-slate-900 font-medium truncate leading-tight">
                                        {m.email}
                                    </div>
                                    <div className="text-[11px] text-slate-400 truncate font-mono leading-tight">
                                        joined {new Date(m.joined_at).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" })}
                                    </div>
                                </div>
                                <span className="text-[10.5px] uppercase tracking-[0.1em] font-medium text-slate-500">
                                    {m.role}
                                </span>
                                {m.role !== "owner" && (
                                    <button
                                        type="button"
                                        onClick={() => remove(m.user_id, m.email || "this member")}
                                        aria-label="Remove member"
                                        className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors opacity-0 group-hover:opacity-100"
                                    >
                                        <TrashIcon className="w-3 h-3" />
                                    </button>
                                )}
                            </div>
                        ))}
                    </div>
                )}
            </div>

            <SectionBar label="Pending invitations" count={inviteList.length} />
            <PageBody>
                {invitations.isPending ? (
                    <SkeletonRows />
                ) : inviteList.length === 0 ? (
                    <div className="px-5 py-10 text-center">
                        <p className="text-[12.5px] text-slate-700 font-medium mb-1">
                            No pending invitations
                        </p>
                        <p className="text-[11.5px] text-slate-400">
                            Invitations show up here until they're accepted.
                        </p>
                    </div>
                ) : (
                    <div className="divide-y divide-slate-200/60">
                        {inviteList.map((inv) => (
                            <div
                                key={inv.id}
                                className="group h-11 px-5 flex items-center gap-3"
                            >
                                <MailIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                                <div className="min-w-0 flex-1 flex items-baseline gap-2">
                                    <span className="text-[12.5px] text-slate-900 truncate">
                                        {inv.email}
                                    </span>
                                    <span className="text-[10px] uppercase tracking-[0.1em] font-medium text-slate-500">
                                        {inv.role}
                                    </span>
                                </div>
                                <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                                    expires {new Date(inv.expires_at).toLocaleDateString("en-US", { month: "short", day: "numeric" })}
                                </span>
                                <button
                                    type="button"
                                    onClick={() => cancel(inv.id)}
                                    aria-label="Cancel invitation"
                                    className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors opacity-0 group-hover:opacity-100"
                                >
                                    <TrashIcon className="w-3 h-3" />
                                </button>
                            </div>
                        ))}
                    </div>
                )}
            </PageBody>

            <InviteDialog open={open} onClose={() => setOpen(false)} />
        </Page>
    );
}

function SkeletonRows() {
    return (
        <div className="divide-y divide-slate-200/60">
            {Array.from({ length: 3 }).map((_, i) => (
                <div key={i} className="h-11 px-5 flex items-center gap-3">
                    <div className="size-6 rounded-full bg-slate-100" />
                    <div className="h-3 w-32 bg-slate-100 rounded animate-pulse" />
                    <div className="ml-auto h-3 w-16 bg-slate-100 rounded animate-pulse" />
                </div>
            ))}
        </div>
    );
}

function InviteDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
    const invite = useInviteMember();
    const [email, setEmail] = React.useState("");
    const [role, setRole] = React.useState<"admin" | "member">("member");

    React.useEffect(() => {
        if (!open) {
            setEmail("");
            setRole("member");
        }
    }, [open]);

    function isValidEmail(s: string) {
        return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s);
    }

    async function submit() {
        const e = email.trim();
        if (!isValidEmail(e)) {
            toast.error("Enter a valid email");
            return;
        }
        try {
            await toast.promise(invite.mutateAsync({ email: e, role }), {
                loading: "Sending invite…",
                success: "Invitation sent",
                error: (err: AppError) => buildError(err),
            });
            onClose();
        } catch {
            /* surfaced */
        }
    }

    return (
        <AnimatePresence>
            {open && (
                <motion.div
                    key="overlay"
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    exit={{ opacity: 0 }}
                    transition={{ duration: 0.15 }}
                    onClick={onClose}
                    className="fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
                >
                    <motion.div
                        key="card"
                        initial={{ y: 8, opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: 8, opacity: 0 }}
                        transition={{ duration: 0.16 }}
                        onClick={(e) => e.stopPropagation()}
                        className="w-full max-w-[460px] rounded-lg bg-white border border-slate-200 shadow-[0_24px_48px_-12px_rgba(15,23,42,0.18)] overflow-hidden"
                    >
                        <div className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5">
                            <div className="size-5 rounded bg-slate-100 text-slate-600 flex items-center justify-center">
                                <UsersIcon className="w-3 h-3" />
                            </div>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                Team
                            </span>
                            <div className="h-4 w-px bg-slate-200" />
                            <span className="text-[12.5px] text-slate-900 font-medium">
                                Invite a member
                            </span>
                            <button
                                type="button"
                                onClick={onClose}
                                aria-label="Close"
                                className="ml-auto size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                            >
                                <XIcon className="w-3.5 h-3.5" />
                            </button>
                        </div>

                        <div className="px-4 py-4 space-y-3">
                            <div>
                                <Label>Email</Label>
                                <TextInput
                                    value={email}
                                    onChange={setEmail}
                                    placeholder="teammate@company.com"
                                    type="email"
                                    autoFocus
                                    className="w-full"
                                />
                            </div>
                            <div>
                                <Label>Role</Label>
                                <PopoverMenu align="start">
                                    <PopoverMenuTrigger asChild>
                                        <SelectButton
                                            label={ROLES.find((r) => r.id === role)?.label ?? "Member"}
                                            className="w-full"
                                        />
                                    </PopoverMenuTrigger>
                                    <PopoverMenuContent minWidth={300}>
                                        <PopoverMenuLabel>Role</PopoverMenuLabel>
                                        {ROLES.map((r) => (
                                            <PopoverMenuItem
                                                key={r.id}
                                                onSelect={() => setRole(r.id)}
                                                selected={role === r.id}
                                            >
                                                <div className="flex flex-col items-start min-w-0">
                                                    <span className="font-medium">{r.label}</span>
                                                    <span className="text-[11px] text-slate-400">
                                                        {r.description}
                                                    </span>
                                                </div>
                                            </PopoverMenuItem>
                                        ))}
                                    </PopoverMenuContent>
                                </PopoverMenu>
                            </div>
                            <p className="text-[11px] text-slate-400 leading-relaxed pt-1">
                                We'll send them a one-click link to join your workspace.
                            </p>
                        </div>

                        <div className="px-3 h-12 border-t border-slate-200 flex items-center gap-1.5">
                            <button
                                type="button"
                                onClick={onClose}
                                className="ml-auto h-7 px-2.5 rounded-md text-[12px] text-slate-700 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                type="button"
                                onClick={submit}
                                disabled={invite.isPending}
                                className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                            >
                                {invite.isPending ? (
                                    <Loader2Icon className="w-3 h-3 animate-spin" />
                                ) : (
                                    <CheckIcon className="w-3 h-3" />
                                )}
                                Send invite
                            </button>
                        </div>
                    </motion.div>
                </motion.div>
            )}
        </AnimatePresence>
    );
}
