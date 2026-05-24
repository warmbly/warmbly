// Members — invitation flow + roster with inline role change.
//
// Restructured to the flat Section + Row shape so the page reads as
// a single document, not a stack of cards. The role pill on each row
// is clickable (for the owner): pops a picker, PATCH /organization/
// members/:id with the new role, and the matrix at /app/settings/
// roles re-renders accordingly.

import React from "react";
import {
    CheckIcon,
    ChevronDownIcon,
    CopyIcon,
    Loader2Icon,
    MailIcon,
    SendIcon,
    ShieldCheckIcon,
    TrashIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { AnimatePresence, motion } from "framer-motion";
import { Label, TextInput } from "@/components/ui/field";
import { useConfirm } from "@/hooks/context/confirm";
import useClickOutside from "@/hooks/useClickOutside";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import useMembers from "@/lib/api/hooks/app/organizations/useMembers";
import usePendingInvitations from "@/lib/api/hooks/app/organizations/usePendingInvitations";
import useInviteMember from "@/lib/api/hooks/app/organizations/useInviteMember";
import useRemoveMember from "@/lib/api/hooks/app/organizations/useRemoveMember";
import useCancelInvitation from "@/lib/api/hooks/app/organizations/useCancelInvitation";
import useUpdateMemberRole from "@/lib/api/hooks/app/organizations/useUpdateMemberRole";
import { useAppStore } from "@/stores";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { ROLE_CATALOG, getRoleDef } from "@/lib/permissions";
import {
    RolePill,
    Section,
    SectionShell,
    TableSurface,
    initials,
    safeEmail,
} from "../_components/SectionShell";

function isValidEmail(s: string) {
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(s);
}

export default function MembersSettingsPage() {
    const confirm = useConfirm();
    const access = useFeatureAccess();
    const members = useMembers();
    const invites = usePendingInvitations();
    const invite = useInviteMember();
    const removeMember = useRemoveMember();
    const cancelInvite = useCancelInvitation();
    const updateRole = useUpdateMemberRole();
    const currentUserId = useAppStore((s) => s.user?.id);
    const currentOrg = useAppStore((s) => s.currentOrganization);

    const memberList = members.data ?? [];
    const inviteList = invites.data ?? [];

    function doRemove(id: string, label: string) {
        confirm?.show(`Remove ${label}?`, async () => {
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
    }
    function doCancelInvite(id: string) {
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
    }
    function copyInviteLink(invitationId: string) {
        const url = `${window.location.origin}/select-org?invitation=${invitationId}`;
        navigator.clipboard.writeText(url).then(
            () => toast.success("Invite link copied"),
            () => toast.error("Couldn't copy"),
        );
    }
    function changeRole(memberId: string, nextRole: string, email: string) {
        const def = getRoleDef(nextRole);
        confirm?.show(`Change ${email}'s role to ${def.label}?`, async () => {
            try {
                await toast.promise(
                    updateRole.mutateAsync({
                        memberId,
                        data: { role: nextRole },
                    }),
                    {
                        loading: "Saving…",
                        success: `Role updated to ${def.label}`,
                        error: (e: AppError) => buildError(e),
                    },
                );
            } catch {
                /* surfaced */
            }
        });
    }

    return (
        <SectionShell
            title="Members"
            description={`Everyone with access to ${currentOrg?.name ?? "this workspace"}.`}
        >
            {access.isOwner && (
                <Section
                    eyebrow="Invite teammates"
                    description="Paste any number of emails — we'll separate them automatically. Pick a role; you can change it later."
                >
                    <InviteFlow
                        pending={invite.isPending}
                        onSubmit={async (emails, role) => {
                            let ok = 0;
                            let failed = 0;
                            for (const e of emails) {
                                try {
                                    await invite.mutateAsync({ email: e, role });
                                    ok++;
                                } catch {
                                    failed++;
                                }
                            }
                            if (ok && !failed) toast.success(`Invited ${ok} ${ok === 1 ? "person" : "people"}`);
                            else if (ok && failed) toast.success(`Invited ${ok} · ${failed} failed`);
                            else toast.error("All invitations failed");
                        }}
                    />
                </Section>
            )}

            <Section
                eyebrow="Members"
                description={`${memberList.length} ${memberList.length === 1 ? "member" : "members"} in this workspace.`}
            >
                {members.isPending ? (
                    <p className="text-[11.5px] text-slate-400 py-2">Loading…</p>
                ) : memberList.length === 0 ? (
                    <p className="text-[11.5px] text-slate-400 py-2">No members yet.</p>
                ) : (
                    <TableSurface>
                        <table className="w-full text-left">
                            <thead>
                                <tr className="border-b border-slate-200">
                                    <Th>Member</Th>
                                    <Th className="w-44">Role</Th>
                                    <Th className="w-40">Joined</Th>
                                    <th className="w-12 px-3 py-2"></th>
                                </tr>
                            </thead>
                            <tbody>
                                {memberList.map((m) => {
                                    const email = safeEmail(m.email) || `(user ${m.user_id.slice(0, 8)})`;
                                    const isSelf = m.user_id === currentUserId;
                                    const isOwner = m.role === "owner";
                                    return (
                                        <tr
                                            key={m.user_id}
                                            className="group h-12 border-b border-slate-200/60 last:border-0 hover:bg-slate-50/80 transition-colors"
                                        >
                                            <td className="px-4">
                                                <div className="flex items-center gap-2.5">
                                                    <div className="size-7 rounded-full bg-slate-100 flex items-center justify-center shrink-0">
                                                        <span className="text-[10px] font-semibold text-slate-600">
                                                            {initials(m.email, m.user_id)}
                                                        </span>
                                                    </div>
                                                    <div className="min-w-0">
                                                        <div className="text-[12.5px] text-slate-900 truncate leading-tight">
                                                            {email}
                                                            {isSelf && (
                                                                <span className="ml-1.5 text-[10px] text-slate-400 uppercase tracking-[0.1em]">
                                                                    you
                                                                </span>
                                                            )}
                                                        </div>
                                                        <div className="text-[10.5px] text-slate-400 truncate font-mono leading-tight">
                                                            {m.user_id.slice(0, 8)}
                                                        </div>
                                                    </div>
                                                </div>
                                            </td>
                                            <td className="px-3">
                                                {access.isOwner && !isOwner ? (
                                                    <InlineRolePicker
                                                        value={m.role}
                                                        onChange={(next) => changeRole(m.user_id, next, email)}
                                                        pending={updateRole.isPending}
                                                    />
                                                ) : isOwner ? (
                                                    <span className="inline-flex items-center gap-1 text-[10px] uppercase tracking-[0.08em] font-semibold rounded-sm px-1.5 py-0.5 border bg-sky-50 text-sky-700 border-sky-100">
                                                        <ShieldCheckIcon className="w-2.5 h-2.5" />
                                                        Owner
                                                    </span>
                                                ) : (
                                                    <RolePill role={m.role} />
                                                )}
                                            </td>
                                            <td className="px-3 font-mono text-[11px] text-slate-500 tabular-nums">
                                                {m.joined_at
                                                    ? new Date(m.joined_at).toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" })
                                                    : "—"}
                                            </td>
                                            <td className="px-3">
                                                {access.isOwner && !isOwner && !isSelf && (
                                                    <button
                                                        type="button"
                                                        onClick={() => doRemove(m.user_id, email)}
                                                        aria-label="Remove member"
                                                        className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors opacity-0 group-hover:opacity-100"
                                                    >
                                                        <TrashIcon className="w-3 h-3" />
                                                    </button>
                                                )}
                                            </td>
                                        </tr>
                                    );
                                })}
                            </tbody>
                        </table>
                    </TableSurface>
                )}
            </Section>

            <Section
                eyebrow="Pending invitations"
                description={`${inviteList.length} awaiting acceptance.`}
            >
                {invites.isPending ? (
                    <p className="text-[11.5px] text-slate-400 py-2">Loading…</p>
                ) : inviteList.length === 0 ? (
                    <p className="text-[11.5px] text-slate-400 py-2">Nothing pending. Invite somebody above.</p>
                ) : (
                    <TableSurface>
                        <table className="w-full text-left">
                            <thead>
                                <tr className="border-b border-slate-200">
                                    <Th>Email</Th>
                                    <Th className="w-32">Role</Th>
                                    <Th className="w-40">Expires</Th>
                                    <th className="w-32 px-3 py-2"></th>
                                </tr>
                            </thead>
                            <tbody>
                                {inviteList.map((inv) => (
                                    <tr
                                        key={inv.id}
                                        className="group h-11 border-b border-slate-200/60 last:border-0 hover:bg-slate-50/80 transition-colors"
                                    >
                                        <td className="px-4">
                                            <div className="flex items-center gap-2.5">
                                                <MailIcon className="w-3.5 h-3.5 text-slate-400" />
                                                <span className="text-[12.5px] text-slate-900 truncate">
                                                    {inv.email}
                                                </span>
                                            </div>
                                        </td>
                                        <td className="px-3">
                                            <RolePill role={inv.role} />
                                        </td>
                                        <td className="px-3 font-mono text-[11px] text-slate-500 tabular-nums">
                                            {new Date(inv.expires_at).toLocaleDateString("en-US", { month: "short", day: "numeric" })}
                                        </td>
                                        <td className="px-3">
                                            {access.isOwner && (
                                                <div className="flex items-center gap-0.5 justify-end opacity-0 group-hover:opacity-100 transition-opacity">
                                                    <button
                                                        type="button"
                                                        onClick={() => copyInviteLink(inv.id)}
                                                        aria-label="Copy invite link"
                                                        title="Copy invite link"
                                                        className="size-6 rounded text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                                                    >
                                                        <CopyIcon className="w-3 h-3" />
                                                    </button>
                                                    <button
                                                        type="button"
                                                        onClick={() => doCancelInvite(inv.id)}
                                                        aria-label="Cancel invitation"
                                                        title="Cancel invitation"
                                                        className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors"
                                                    >
                                                        <TrashIcon className="w-3 h-3" />
                                                    </button>
                                                </div>
                                            )}
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </TableSurface>
                )}
            </Section>
        </SectionShell>
    );
}

function Th({ children, className }: { children: React.ReactNode; className?: string }) {
    return (
        <th
            className={`px-3 py-2 text-[10px] font-medium text-slate-400 uppercase tracking-[0.14em] ${className ?? ""}`}
        >
            {children}
        </th>
    );
}

const ACCENT_PILL: Record<string, string> = {
    sky:     "bg-sky-50 text-sky-700 border-sky-100",
    violet:  "bg-violet-50 text-violet-700 border-violet-100",
    emerald: "bg-emerald-50 text-emerald-700 border-emerald-100",
    slate:   "bg-slate-50 text-slate-700 border-slate-200",
    amber:   "bg-amber-50 text-amber-700 border-amber-100",
};
const ACCENT_DOT: Record<string, string> = {
    sky:     "bg-sky-500",
    violet:  "bg-violet-500",
    emerald: "bg-emerald-500",
    slate:   "bg-slate-400",
    amber:   "bg-amber-500",
};

function InlineRolePicker({
    value,
    onChange,
    pending,
}: {
    value: string;
    onChange: (next: string) => void;
    pending: boolean;
}) {
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
    const cur = getRoleDef(value);
    const assignable = ROLE_CATALOG.filter((r) => r.assignable && r.id !== "member");

    return (
        <div ref={ref} className="relative inline-block">
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                disabled={pending}
                className={`h-6 px-1.5 rounded text-[10px] uppercase tracking-[0.08em] font-semibold inline-flex items-center gap-1 border transition-colors ${ACCENT_PILL[cur.accent] ?? ACCENT_PILL.slate} hover:opacity-80 disabled:opacity-60`}
            >
                {pending ? (
                    <Loader2Icon className="w-2.5 h-2.5 animate-spin" />
                ) : (
                    <span className={`size-1.5 rounded-full ${ACCENT_DOT[cur.accent] ?? ACCENT_DOT.slate}`} />
                )}
                {cur.label}
                <ChevronDownIcon className="w-2.5 h-2.5 opacity-60" />
            </button>
            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute top-full left-0 mt-1 z-20 w-72 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] py-1"
                    >
                        {assignable.map((r) => {
                            const selected = r.id === value;
                            return (
                                <button
                                    key={r.id}
                                    type="button"
                                    onClick={() => {
                                        setOpen(false);
                                        onChange(r.id);
                                    }}
                                    className={`w-full px-2.5 py-1.5 text-left hover:bg-slate-100 transition-colors ${
                                        selected ? "bg-slate-50" : ""
                                    }`}
                                >
                                    <div className="flex items-center gap-2">
                                        <span className={`size-1.5 rounded-full ${ACCENT_DOT[r.accent] ?? ACCENT_DOT.slate}`} />
                                        <span className="text-[12px] font-medium text-slate-900">
                                            {r.label}
                                        </span>
                                        {selected && <CheckIcon className="ml-auto w-3 h-3 text-slate-500" />}
                                    </div>
                                    <p className="text-[11px] text-slate-500 leading-tight mt-0.5">
                                        {r.description}
                                    </p>
                                </button>
                            );
                        })}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

/**
 * Multi-email invite flow. Email chips + role selector + send button,
 * with the role-description rail on the right so the owner sees what
 * they're granting before committing.
 */
function InviteFlow({
    onSubmit,
    pending,
}: {
    onSubmit: (emails: string[], role: string) => Promise<void>;
    pending: boolean;
}) {
    const [chips, setChips] = React.useState<{ email: string; valid: boolean }[]>([]);
    const [draft, setDraft] = React.useState("");
    const [role, setRole] = React.useState<string>("manager");
    const SEPARATOR_RE = /[\s,;]+/;

    function commitDrafts(value: string) {
        const parts = value.split(SEPARATOR_RE).map((s) => s.trim()).filter(Boolean);
        if (parts.length === 0) return;
        setChips((curr) => {
            const seen = new Set(curr.map((c) => c.email.toLowerCase()));
            const next = [...curr];
            for (const p of parts) {
                const key = p.toLowerCase();
                if (seen.has(key)) continue;
                seen.add(key);
                next.push({ email: p, valid: isValidEmail(p) });
            }
            return next;
        });
    }

    function onKey(e: React.KeyboardEvent<HTMLInputElement>) {
        if (e.key === "Enter") {
            e.preventDefault();
            commitDrafts(draft);
            setDraft("");
        } else if (e.key === "," || e.key === ";" || e.key === " ") {
            if (draft.trim()) {
                e.preventDefault();
                commitDrafts(draft);
                setDraft("");
            }
        } else if (e.key === "Backspace" && draft === "") {
            setChips((c) => c.slice(0, -1));
        }
    }

    function removeChip(email: string) {
        setChips((c) => c.filter((x) => x.email !== email));
    }

    async function submit() {
        if (draft.trim()) commitDrafts(draft);
        const final = [
            ...chips.map((c) => c.email),
            ...draft.split(SEPARATOR_RE).map((s) => s.trim()).filter(Boolean),
        ];
        const unique = Array.from(new Set(final.map((s) => s.toLowerCase())));
        const valid = unique.filter(isValidEmail);
        const invalid = unique.filter((e) => !isValidEmail(e));

        if (invalid.length > 0 && valid.length === 0) {
            toast.error("No valid email addresses entered");
            return;
        }
        if (invalid.length > 0) {
            toast(`Skipping ${invalid.length} invalid: ${invalid.slice(0, 3).join(", ")}${invalid.length > 3 ? "…" : ""}`, {
                icon: "⚠️",
            });
        }
        await onSubmit(valid, role);
        setChips([]);
        setDraft("");
    }

    const totalCount = chips.length + (draft.trim() ? draft.trim().split(SEPARATOR_RE).filter(Boolean).length : 0);
    const assignable = ROLE_CATALOG.filter((r) => r.assignable && r.id !== "member");
    const activeRole = getRoleDef(role);

    return (
        <div className="grid grid-cols-1 lg:grid-cols-[1fr_220px] gap-4">
            <div className="space-y-3">
                <div>
                    <Label>Emails</Label>
                    <div
                        className="min-h-[36px] w-full px-2 py-1.5 rounded-md border border-slate-200 bg-white flex flex-wrap items-center gap-1.5 focus-within:border-sky-400 focus-within:ring-2 focus-within:ring-sky-100 transition-colors"
                        onClick={(e) => {
                            const input = (e.currentTarget as HTMLDivElement).querySelector("input");
                            input?.focus();
                        }}
                    >
                        {chips.map((c) => (
                            <span
                                key={c.email}
                                className={`inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] font-medium ${
                                    c.valid ? "bg-slate-100 text-slate-700" : "bg-red-50 text-red-700"
                                }`}
                            >
                                {c.email}
                                <button
                                    type="button"
                                    onClick={(ev) => {
                                        ev.stopPropagation();
                                        removeChip(c.email);
                                    }}
                                    className="opacity-60 hover:opacity-100"
                                    aria-label={`Remove ${c.email}`}
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
                        ))}
                        <input
                            value={draft}
                            onChange={(e) => setDraft(e.target.value)}
                            onKeyDown={onKey}
                            onPaste={(e) => {
                                const text = e.clipboardData.getData("text");
                                if (SEPARATOR_RE.test(text)) {
                                    e.preventDefault();
                                    commitDrafts(text);
                                    setDraft("");
                                }
                            }}
                            onBlur={() => {
                                if (draft.trim()) {
                                    commitDrafts(draft);
                                    setDraft("");
                                }
                            }}
                            placeholder={chips.length === 0 ? "name@company.com, another@…" : ""}
                            className="flex-1 min-w-[120px] h-5 bg-transparent text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none"
                        />
                    </div>
                    <p className="text-[11px] text-slate-400 mt-1">
                        Separate with comma, semicolon, space or enter. Paste any list — we'll split it.
                    </p>
                </div>

                <div className="flex items-center gap-2">
                    <Label className="!mb-0 w-16">Role</Label>
                    <div className="inline-flex items-center rounded-md border border-slate-200 bg-white p-0.5 shrink-0 flex-wrap">
                        {assignable.map((r) => (
                            <button
                                key={r.id}
                                type="button"
                                onClick={() => setRole(r.id)}
                                className={`h-6 px-2.5 rounded text-[11.5px] font-medium transition-colors ${
                                    role === r.id
                                        ? "bg-slate-900 text-white"
                                        : "text-slate-500 hover:text-slate-900"
                                }`}
                            >
                                {r.label}
                            </button>
                        ))}
                    </div>
                </div>

                <div className="flex items-center gap-2 pt-1">
                    <span className="text-[11px] text-slate-500">
                        {totalCount > 0
                            ? `${totalCount} ${totalCount === 1 ? "email" : "emails"} ready`
                            : "Enter at least one email."}
                    </span>
                    <button
                        type="button"
                        onClick={submit}
                        disabled={pending || (chips.length === 0 && draft.trim() === "")}
                        className="ml-auto h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {pending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <SendIcon className="w-3 h-3" />}
                        Send invitations
                    </button>
                </div>
            </div>

            <div className="rounded-md border border-slate-200 bg-slate-50/40 p-3">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                    <CheckIcon className="w-3 h-3 text-slate-400" />
                    {activeRole.label}
                </div>
                <p className="text-[12px] text-slate-700 leading-relaxed">
                    {activeRole.description}
                </p>
            </div>
        </div>
    );
}
