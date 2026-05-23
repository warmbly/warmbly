// Settings — two-pane sheet.
//
//   ┌────────────────┬─────────────────────────────────────────┐
//   │ left nav rail  │ active section panel                    │
//   │ Profile        │                                         │
//   │ Notifications  │                                         │
//   │ Security       │                                         │
//   │ Members        │                                         │
//   │ Workspace      │                                         │
//   │ Danger zone    │                                         │
//   └────────────────┴─────────────────────────────────────────┘
//
// Each section is a small component below; the rail just switches
// which one renders on the right. Section is held in URL hash so
// links like /app/settings#members work directly.

import React from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import {
    AlertOctagonIcon,
    BellIcon,
    BriefcaseIcon,
    Loader2Icon,
    ShieldIcon,
    Trash2Icon,
    UserIcon,
    UsersIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { Page, PageTopbar, TopbarAction } from "@/components/layout/Page";
import { Label, TextInput } from "@/components/ui/field";
import { useUserProfile } from "@/hooks/context/user";
import { useAppStore } from "@/stores";
import { comingSoon } from "@/lib/helper/comingSoon";
import useMembers from "@/lib/api/hooks/app/organizations/useMembers";
import usePendingInvitations from "@/lib/api/hooks/app/organizations/usePendingInvitations";
import useInviteMember from "@/lib/api/hooks/app/organizations/useInviteMember";
import useFeatureAccess from "@/hooks/useFeatureAccess";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

type SectionId =
    | "profile"
    | "notifications"
    | "security"
    | "members"
    | "workspace"
    | "danger";

interface SectionDef {
    id: SectionId;
    label: string;
    icon: React.ComponentType<{ className?: string }>;
    description: string;
    /** Owner-only sections are hidden from non-owners. */
    ownerOnly?: boolean;
}

const SECTIONS: SectionDef[] = [
    { id: "profile",       label: "Profile",       icon: UserIcon,         description: "Personal information and avatar." },
    { id: "notifications", label: "Notifications", icon: BellIcon,         description: "What you get notified about." },
    { id: "security",      label: "Security",      icon: ShieldIcon,       description: "Password, 2FA, active sessions." },
    { id: "members",       label: "Members",       icon: UsersIcon,        description: "Team and invitations." },
    { id: "workspace",     label: "Workspace",     icon: BriefcaseIcon,    description: "Org-wide settings.", ownerOnly: true },
    { id: "danger",        label: "Danger zone",   icon: AlertOctagonIcon, description: "Irreversible actions." },
];

export default function SettingsPage() {
    const navigate = useNavigate();
    const location = useLocation();
    const access = useFeatureAccess();

    const sectionFromHash = (location.hash.replace("#", "") || "profile") as SectionId;
    const activeSection: SectionId =
        SECTIONS.some((s) => s.id === sectionFromHash) ? sectionFromHash : "profile";

    const visibleSections = SECTIONS.filter((s) => !s.ownerOnly || access.isOwner);
    const current = visibleSections.find((s) => s.id === activeSection) ?? visibleSections[0];

    function go(id: SectionId) {
        navigate(`#${id}`, { replace: true });
    }

    return (
        <Page>
            <PageTopbar
                eyebrow="Settings"
                subtitle={current?.description ?? "Account and workspace"}
            />

            <div className="flex-1 min-h-0 flex">
                <nav className="w-[200px] shrink-0 border-r border-slate-200/70 py-2 overflow-y-auto">
                    {visibleSections.map((s) => {
                        const active = current?.id === s.id;
                        return (
                            <button
                                key={s.id}
                                type="button"
                                onClick={() => go(s.id)}
                                className={`group block w-[calc(100%-0.75rem)] mx-1.5 my-px flex items-center gap-2 px-2 h-7 rounded text-[12.5px] text-left transition-colors ${
                                    active
                                        ? "bg-slate-200/70 text-slate-900 font-medium"
                                        : "text-slate-600 hover:text-slate-900 hover:bg-slate-200/40"
                                }`}
                            >
                                <s.icon
                                    className={`w-[14px] h-[14px] shrink-0 ${
                                        active ? "text-slate-700" : "text-slate-400 group-hover:text-slate-600"
                                    }`}
                                />
                                <span className="truncate">{s.label}</span>
                            </button>
                        );
                    })}
                </nav>

                <div className="flex-1 min-w-0 overflow-y-auto">
                    {current?.id === "profile" && <ProfileSection />}
                    {current?.id === "notifications" && <NotificationsSection />}
                    {current?.id === "security" && <SecuritySection />}
                    {current?.id === "members" && <MembersSection />}
                    {current?.id === "workspace" && <WorkspaceSection />}
                    {current?.id === "danger" && <DangerSection />}
                </div>
            </div>
        </Page>
    );
}

function SectionHeading({ title, description }: { title: string; description?: string }) {
    return (
        <div className="mb-4">
            <h2 className="text-[14px] font-semibold text-slate-900">{title}</h2>
            {description && (
                <p className="text-[12px] text-slate-500 mt-0.5 leading-relaxed max-w-xl">
                    {description}
                </p>
            )}
        </div>
    );
}

function ProfileSection() {
    const { user } = useUserProfile();
    const [firstName, setFirstName] = React.useState(user.first_name ?? "");
    const [lastName, setLastName] = React.useState(user.last_name ?? "");

    return (
        <div className="px-6 py-5 max-w-xl">
            <SectionHeading
                title="Profile"
                description="Used across invitations, emails sent on your behalf, and your sidebar avatar."
            />
            <div className="space-y-3">
                <div className="grid grid-cols-2 gap-2">
                    <div>
                        <Label>First name</Label>
                        <TextInput value={firstName} onChange={setFirstName} className="w-full" />
                    </div>
                    <div>
                        <Label>Last name</Label>
                        <TextInput value={lastName} onChange={setLastName} className="w-full" />
                    </div>
                </div>
                <div>
                    <Label>Email</Label>
                    <input
                        type="email"
                        value={user.email}
                        disabled
                        className="w-full h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12.5px] text-slate-500"
                    />
                    <p className="text-[11px] text-slate-400 mt-1">Email changes go through support for now.</p>
                </div>
                <div className="pt-1">
                    <TopbarAction onClick={() => comingSoon("Profile editing")}>Save profile</TopbarAction>
                </div>
            </div>
        </div>
    );
}

function NotificationsSection() {
    return (
        <div className="px-6 py-5 max-w-xl">
            <SectionHeading title="Notifications" description="Email + in-app alerts. Change any time." />
            <div className="space-y-1">
                <ToggleRow label="Reply received" description="When a recipient replies to a campaign." />
                <ToggleRow label="Bounce detected" description="When a mailbox starts bouncing hard." />
                <ToggleRow label="Spam complaint" description="Immediate alert on any complaint event." defaultOn />
                <ToggleRow label="Weekly digest" description="Monday summary of last week's volume and replies." defaultOn />
                <ToggleRow label="Worker downtime" description="If one of your sender workers stops responding." defaultOn />
            </div>
        </div>
    );
}

function SecuritySection() {
    return (
        <div className="px-6 py-5 max-w-xl">
            <SectionHeading title="Security" description="Sign-in protection for your account." />
            <div className="space-y-2">
                <RowLink title="Active sessions" description="Devices currently signed in to your account." cta="View sessions" />
                <RowLink title="Two-factor authentication" description="Add a one-time code to every sign-in." cta="Enable 2FA" />
                <RowLink title="Change password" description="Use 12+ characters with mixed case and a number." cta="Change" />
            </div>
        </div>
    );
}

function MembersSection() {
    const members = useMembers();
    const invites = usePendingInvitations();
    const invite = useInviteMember();
    const access = useFeatureAccess();

    const memberList = members.data ?? [];
    const inviteList = invites.data ?? [];

    const [email, setEmail] = React.useState("");
    const [role, setRole] = React.useState<"admin" | "member">("member");
    const canInvite = access.isOwner;

    async function submit() {
        const e = email.trim();
        if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(e)) {
            toast.error("Enter a valid email");
            return;
        }
        try {
            await toast.promise(invite.mutateAsync({ email: e, role }), {
                loading: "Sending invite…",
                success: "Invitation sent",
                error: (err: AppError) => buildError(err),
            });
            setEmail("");
        } catch {
            /* surfaced */
        }
    }

    return (
        <div className="px-6 py-5 max-w-2xl">
            <SectionHeading
                title="Members"
                description="Everyone with access to this workspace. Owners can change roles and invite."
            />

            {canInvite && (
                <div className="mb-5 rounded-md border border-slate-200 bg-white">
                    <div className="h-9 px-3 border-b border-slate-200 flex items-center gap-1.5">
                        <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                            Invite by email
                        </span>
                    </div>
                    <div className="px-3 py-3 flex items-center gap-2">
                        <TextInput
                            value={email}
                            onChange={setEmail}
                            placeholder="teammate@company.com"
                            type="email"
                            className="flex-1"
                        />
                        <div className="inline-flex items-center rounded-md border border-slate-200 bg-white p-0.5 shrink-0">
                            {(["admin", "member"] as const).map((r) => (
                                <button
                                    key={r}
                                    type="button"
                                    onClick={() => setRole(r)}
                                    className={`h-6 px-2 rounded text-[11.5px] font-medium transition-colors capitalize ${
                                        role === r ? "bg-slate-900 text-white" : "text-slate-500 hover:text-slate-900"
                                    }`}
                                >
                                    {r}
                                </button>
                            ))}
                        </div>
                        <button
                            type="button"
                            onClick={submit}
                            disabled={invite.isPending}
                            className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60 shrink-0"
                        >
                            {invite.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            Invite
                        </button>
                    </div>
                </div>
            )}

            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                Members <span className="font-mono tabular-nums">{memberList.length}</span>
            </div>
            <div className="rounded-md border border-slate-200 bg-white overflow-hidden divide-y divide-slate-200/60 mb-5">
                {members.isPending ? (
                    <div className="px-3 py-3 text-[11.5px] text-slate-400">Loading…</div>
                ) : memberList.length === 0 ? (
                    <div className="px-3 py-3 text-[11.5px] text-slate-400">No members yet.</div>
                ) : (
                    memberList.map((m) => (
                        <div key={m.user_id} className="px-3 h-10 flex items-center gap-2.5">
                            <div className="size-6 rounded-full bg-slate-100 flex items-center justify-center shrink-0">
                                <span className="text-[9.5px] font-semibold text-slate-600">
                                    {m.email.slice(0, 2).toUpperCase()}
                                </span>
                            </div>
                            <span className="text-[12px] text-slate-900 truncate flex-1">{m.email}</span>
                            <span className="text-[10.5px] uppercase tracking-[0.08em] font-medium text-slate-500">
                                {m.role}
                            </span>
                        </div>
                    ))
                )}
            </div>

            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1.5 flex items-center gap-1.5">
                Pending invitations <span className="font-mono tabular-nums">{inviteList.length}</span>
            </div>
            <div className="rounded-md border border-slate-200 bg-white overflow-hidden divide-y divide-slate-200/60">
                {invites.isPending ? (
                    <div className="px-3 py-3 text-[11.5px] text-slate-400">Loading…</div>
                ) : inviteList.length === 0 ? (
                    <div className="px-3 py-3 text-[11.5px] text-slate-400">No pending invitations.</div>
                ) : (
                    inviteList.map((inv) => (
                        <div key={inv.id} className="px-3 h-10 flex items-center gap-2.5">
                            <span className="text-[12px] text-slate-900 truncate flex-1">{inv.email}</span>
                            <span className="text-[10.5px] uppercase tracking-[0.08em] font-medium text-slate-500">
                                {inv.role}
                            </span>
                            <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                                expires {new Date(inv.expires_at).toLocaleDateString("en-US", { month: "short", day: "numeric" })}
                            </span>
                        </div>
                    ))
                )}
            </div>

            <p className="text-[11px] text-slate-400 mt-3">
                Need finer controls?{" "}
                <Link to="/app/team" className="text-slate-700 underline-offset-2 hover:underline">
                    Open the full team page
                </Link>
                .
            </p>
        </div>
    );
}

function WorkspaceSection() {
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const [name, setName] = React.useState(currentOrg?.name ?? "");
    const [domain, setDomain] = React.useState("");

    return (
        <div className="px-6 py-5 max-w-xl">
            <SectionHeading title="Workspace" description="Org-wide settings. Visible only to the owner." />
            <div className="space-y-3">
                <div>
                    <Label>Workspace name</Label>
                    <TextInput value={name} onChange={setName} className="w-full" />
                </div>
                <div>
                    <Label>Default sender domain</Label>
                    <TextInput value={domain} onChange={setDomain} placeholder="company.com" className="w-full" />
                    <p className="text-[11px] text-slate-400 mt-1">
                        Used when a campaign doesn't explicitly pick a from-domain.
                    </p>
                </div>
                <div className="pt-1">
                    <TopbarAction onClick={() => comingSoon("Workspace settings")}>
                        Save workspace
                    </TopbarAction>
                </div>
            </div>
        </div>
    );
}

function DangerSection() {
    return (
        <div className="px-6 py-5 max-w-xl">
            <SectionHeading title="Danger zone" description="Irreversible actions. Read carefully." />
            <div className="space-y-3">
                <DangerCard
                    title="Delete account"
                    body="Permanently delete your account and every workspace you own. This can't be undone."
                    cta="Delete account…"
                    onClick={() => comingSoon("Account deletion")}
                />
                <DangerCard
                    title="Leave workspace"
                    body="Remove yourself from this workspace. You'll lose access to its data immediately."
                    cta="Leave workspace…"
                    onClick={() => comingSoon("Leave workspace")}
                />
            </div>
        </div>
    );
}

function ToggleRow({
    label,
    description,
    defaultOn,
}: {
    label: string;
    description: string;
    defaultOn?: boolean;
}) {
    const [on, setOn] = React.useState(!!defaultOn);
    return (
        <div className="flex items-center gap-3 py-2.5">
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] text-slate-900 font-medium leading-tight">{label}</div>
                <div className="text-[11.5px] text-slate-500 leading-tight mt-0.5">{description}</div>
            </div>
            <button
                type="button"
                onClick={() => setOn(!on)}
                role="switch"
                aria-checked={on}
                className={`relative h-4 w-7 rounded-full transition-colors shrink-0 ${
                    on ? "bg-slate-900" : "bg-slate-200"
                }`}
            >
                <span
                    className={`absolute top-0.5 left-0.5 size-3 rounded-full bg-white transition-transform ${
                        on ? "translate-x-3" : "translate-x-0"
                    }`}
                />
            </button>
        </div>
    );
}

function RowLink({
    title,
    description,
    cta,
}: {
    title: string;
    description: string;
    cta: string;
}) {
    return (
        <div className="flex items-center gap-3 py-2">
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] text-slate-900 font-medium leading-tight">{title}</div>
                <div className="text-[11.5px] text-slate-500 leading-tight mt-0.5">{description}</div>
            </div>
            <button
                type="button"
                onClick={() => comingSoon(title)}
                className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors shrink-0"
            >
                {cta}
            </button>
        </div>
    );
}

function DangerCard({
    title,
    body,
    cta,
    onClick,
}: {
    title: string;
    body: string;
    cta: string;
    onClick: () => void;
}) {
    return (
        <div className="rounded-md border border-red-200 bg-red-50/40 p-3">
            <div className="text-[12.5px] font-semibold text-red-700 mb-0.5 flex items-center gap-1.5">
                <Trash2Icon className="w-3 h-3" />
                {title}
            </div>
            <p className="text-[11.5px] text-red-700/80 mb-2 leading-relaxed">{body}</p>
            <button
                type="button"
                onClick={onClick}
                className="h-7 px-2.5 rounded-md border border-red-300 hover:border-red-400 text-red-700 hover:text-red-800 hover:bg-red-100/60 text-[12px] font-medium transition-colors"
            >
                {cta}
            </button>
        </div>
    );
}
