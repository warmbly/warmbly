// Teams — group organization members under named, colored labels.
//
// Follows the flat Section + Row shape shared by the rest of the
// Settings outlet (see members/page.tsx + SectionShell). The page
// has two parts: a create form (name + color swatch) at the top, and
// the roster of existing teams below, each with a member picker
// sourced from the org's members and removable member chips.

import React from "react";
import {
    CheckIcon,
    Loader2Icon,
    PlusIcon,
    TrashIcon,
    UserPlusIcon,
    UsersIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { Label, SearchInput, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import useMembers from "@/lib/api/hooks/app/organizations/useMembers";
import useTeams from "@/lib/api/hooks/app/teams/useTeams";
import useCreateTeam from "@/lib/api/hooks/app/teams/useCreateTeam";
import useDeleteTeam from "@/lib/api/hooks/app/teams/useDeleteTeam";
import useAddTeamMember from "@/lib/api/hooks/app/teams/useAddTeamMember";
import useRemoveTeamMember from "@/lib/api/hooks/app/teams/useRemoveTeamMember";
import type Team from "@/lib/api/models/app/teams/Team";
import type OrganizationMember from "@/lib/api/models/app/organizations/OrganizationMember";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { useAppStore } from "@/stores";
import { Section, SectionShell, initials, safeEmail } from "../_components/SectionShell";

const TEAM_COLORS = [
    { id: "slate", bg: "bg-slate-400", hex: "#94a3b8" },
    { id: "sky", bg: "bg-sky-500", hex: "#0ea5e9" },
    { id: "violet", bg: "bg-violet-500", hex: "#8b5cf6" },
    { id: "amber", bg: "bg-amber-500", hex: "#f59e0b" },
    { id: "emerald", bg: "bg-emerald-500", hex: "#10b981" },
    { id: "rose", bg: "bg-rose-500", hex: "#f43f5e" },
    { id: "indigo", bg: "bg-indigo-500", hex: "#6366f1" },
    { id: "teal", bg: "bg-teal-500", hex: "#14b8a6" },
];
const DEFAULT_COLOR = TEAM_COLORS[0].hex;

export default function TeamsSettingsPage() {
    const confirm = useConfirm();
    const currentOrg = useAppStore((s) => s.currentOrganization);
    const teamsQuery = useTeams();
    const membersQuery = useMembers();
    const createTeam = useCreateTeam();
    const deleteTeam = useDeleteTeam();

    const teams = teamsQuery.data ?? [];
    const members = membersQuery.data ?? [];

    function doDelete(team: Team) {
        confirm?.show(`Delete the team "${team.name}"?`, async () => {
            try {
                await toast.promise(deleteTeam.mutateAsync(team.id), {
                    loading: "Deleting…",
                    success: "Team deleted",
                    error: (e: AppError) => buildError(e),
                });
            } catch {
                /* surfaced */
            }
        });
    }

    return (
        <SectionShell
            title="Teams"
            description={`Group members of ${currentOrg?.name ?? "this workspace"} into named teams.`}
        >
            <Section
                eyebrow="Create a team"
                description="Give the team a name and a color, then add members below."
            >
                <CreateTeamForm
                    pending={createTeam.isPending}
                    onSubmit={async (name, color) => {
                        try {
                            await toast.promise(createTeam.mutateAsync({ name, color }), {
                                loading: "Creating…",
                                success: `Team "${name}" created`,
                                error: (e: AppError) => buildError(e),
                            });
                            return true;
                        } catch {
                            return false;
                        }
                    }}
                />
            </Section>

            <Section
                eyebrow="Teams"
                description={`${teams.length} ${teams.length === 1 ? "team" : "teams"} in this workspace.`}
            >
                {teamsQuery.isPending ? (
                    <p className="text-[11.5px] text-slate-400 py-2">Loading…</p>
                ) : teams.length === 0 ? (
                    <p className="text-[11.5px] text-slate-400 py-2">
                        No teams yet. Create one above.
                    </p>
                ) : (
                    <div className="space-y-3">
                        {teams.map((team) => (
                            <TeamCard
                                key={team.id}
                                team={team}
                                members={members}
                                onDelete={() => doDelete(team)}
                            />
                        ))}
                    </div>
                )}
            </Section>
        </SectionShell>
    );
}

function CreateTeamForm({
    pending,
    onSubmit,
}: {
    pending: boolean;
    onSubmit: (name: string, color: string) => Promise<boolean>;
}) {
    const [name, setName] = React.useState("");
    const [color, setColor] = React.useState(DEFAULT_COLOR);

    async function submit() {
        const trimmed = name.trim();
        if (!trimmed) {
            toast.error("Enter a team name");
            return;
        }
        const ok = await onSubmit(trimmed, color);
        if (ok) {
            setName("");
            setColor(DEFAULT_COLOR);
        }
    }

    return (
        <div className="space-y-3">
            <div>
                <Label>Name</Label>
                <div className="flex flex-wrap items-center gap-2">
                    <div className="flex items-center gap-1.5 shrink-0">
                        {TEAM_COLORS.map((c) => (
                            <button
                                key={c.id}
                                type="button"
                                onClick={() => setColor(c.hex)}
                                aria-label={`Use ${c.id}`}
                                className={`size-5 rounded-full ${c.bg} flex items-center justify-center transition-transform hover:scale-110 ${
                                    c.hex.toLowerCase() === color.toLowerCase()
                                        ? "ring-2 ring-offset-1 ring-slate-400"
                                        : ""
                                }`}
                            >
                                {c.hex.toLowerCase() === color.toLowerCase() && (
                                    <CheckIcon className="w-3 h-3 text-white" />
                                )}
                            </button>
                        ))}
                    </div>
                    <TextInput
                        value={name}
                        onChange={setName}
                        placeholder="Sales, Support, Founders…"
                        onKeyDown={(e) => {
                            if (e.key === "Enter") {
                                e.preventDefault();
                                submit();
                            }
                        }}
                        className="flex-1 min-w-[160px]"
                    />
                    <button
                        type="button"
                        onClick={submit}
                        disabled={pending || name.trim() === ""}
                        className="h-7 px-2.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60 shrink-0"
                    >
                        {pending ? (
                            <Loader2Icon className="w-3 h-3 animate-spin" />
                        ) : (
                            <PlusIcon className="w-3 h-3" />
                        )}
                        Create team
                    </button>
                </div>
            </div>
        </div>
    );
}

function TeamCard({
    team,
    members,
    onDelete,
}: {
    team: Team;
    members: OrganizationMember[];
    onDelete: () => void;
}) {
    const addMember = useAddTeamMember();
    const removeMember = useRemoveTeamMember();

    const memberIds = React.useMemo(
        () => new Set(team.members.map((m) => m.user_id)),
        [team.members],
    );
    const available = React.useMemo(
        () => members.filter((m) => !memberIds.has(m.user_id)),
        [members, memberIds],
    );

    async function add(userId: string) {
        try {
            await toast.promise(addMember.mutateAsync({ id: team.id, userId }), {
                loading: "Adding…",
                success: "Member added",
                error: (e: AppError) => buildError(e),
            });
        } catch {
            /* surfaced */
        }
    }
    async function remove(userId: string) {
        try {
            await toast.promise(removeMember.mutateAsync({ id: team.id, userId }), {
                loading: "Removing…",
                success: "Member removed",
                error: (e: AppError) => buildError(e),
            });
        } catch {
            /* surfaced */
        }
    }

    return (
        <div className="rounded-md border border-slate-200 bg-white overflow-hidden">
            <div className="px-3 h-11 flex items-center gap-2.5 border-b border-slate-200">
                <span
                    className="size-2.5 rounded-full shrink-0"
                    style={{ backgroundColor: team.color || DEFAULT_COLOR }}
                />
                <span className="text-[13px] font-semibold text-slate-900 truncate">
                    {team.name}
                </span>
                <span className="text-[11px] text-slate-400 inline-flex items-center gap-1 shrink-0">
                    <UsersIcon className="w-3 h-3" />
                    {team.members.length}
                </span>
                <div className="ml-auto flex items-center gap-1 shrink-0">
                    <AddMemberPicker
                        available={available}
                        pending={addMember.isPending}
                        onAdd={add}
                    />
                    <button
                        type="button"
                        onClick={onDelete}
                        aria-label="Delete team"
                        title="Delete team"
                        className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors"
                    >
                        <TrashIcon className="w-3.5 h-3.5" />
                    </button>
                </div>
            </div>

            <div className="px-3 py-2.5">
                {team.members.length === 0 ? (
                    <p className="text-[11.5px] text-slate-400 py-1">
                        No members yet. Use Add member to assign someone.
                    </p>
                ) : (
                    <div className="flex flex-wrap gap-1.5">
                        {team.members.map((m) => {
                            const label = m.name?.trim() || safeEmail(m.email) || `user ${m.user_id.slice(0, 8)}`;
                            return (
                                <span
                                    key={m.user_id}
                                    className="group inline-flex items-center gap-1.5 h-6 pl-1 pr-1.5 rounded-full border border-slate-200 bg-slate-50 text-[11.5px] text-slate-700"
                                >
                                    <span className="size-4 rounded-full bg-white border border-slate-200 flex items-center justify-center shrink-0">
                                        <span className="text-[8px] font-semibold text-slate-600">
                                            {initials(m.email, m.user_id)}
                                        </span>
                                    </span>
                                    <span className="truncate max-w-[160px]">{label}</span>
                                    <button
                                        type="button"
                                        onClick={() => remove(m.user_id)}
                                        disabled={removeMember.isPending}
                                        aria-label={`Remove ${label}`}
                                        className="size-4 rounded-full text-slate-400 hover:text-red-600 hover:bg-red-100 inline-flex items-center justify-center transition-colors disabled:opacity-50"
                                    >
                                        <XIcon className="w-2.5 h-2.5" />
                                    </button>
                                </span>
                            );
                        })}
                    </div>
                )}
            </div>
        </div>
    );
}

function AddMemberPicker({
    available,
    pending,
    onAdd,
}: {
    available: OrganizationMember[];
    pending: boolean;
    onAdd: (userId: string) => void;
}) {
    const [open, setOpen] = React.useState(false);
    const [query, setQuery] = React.useState("");

    const filtered = React.useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return available;
        return available.filter((m) => safeEmail(m.email).toLowerCase().includes(q));
    }, [available, query]);

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    disabled={pending}
                    className="h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11.5px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1 transition-colors disabled:opacity-60"
                >
                    {pending ? (
                        <Loader2Icon className="w-3 h-3 animate-spin" />
                    ) : (
                        <UserPlusIcon className="w-3 h-3" />
                    )}
                    Add member
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={260} className="max-w-[calc(100vw-2rem)] p-1.5">
                <div className="px-1 pb-1.5">
                    <SearchInput
                        value={query}
                        onChange={setQuery}
                        placeholder="Search members…"
                        autoFocus
                    />
                </div>
                {filtered.length === 0 ? (
                    <p className="px-2 py-3 text-[11.5px] text-slate-400 text-center">
                        {available.length === 0 ? "Everyone is already on this team." : "No matches."}
                    </p>
                ) : (
                    <div className="max-h-56 overflow-y-auto">
                        {filtered.map((m) => {
                            const email = safeEmail(m.email) || `(user ${m.user_id.slice(0, 8)})`;
                            return (
                                <button
                                    key={m.user_id}
                                    type="button"
                                    onClick={() => {
                                        setOpen(false);
                                        setQuery("");
                                        onAdd(m.user_id);
                                    }}
                                    className="w-full px-1.5 py-1.5 rounded text-left hover:bg-slate-100 transition-colors flex items-center gap-2"
                                >
                                    <span className="size-6 rounded-full bg-slate-100 flex items-center justify-center shrink-0">
                                        <span className="text-[9px] font-semibold text-slate-600">
                                            {initials(m.email, m.user_id)}
                                        </span>
                                    </span>
                                    <span className="min-w-0">
                                        <span className="block text-[12px] text-slate-900 truncate leading-tight">
                                            {email}
                                        </span>
                                        <span className="block text-[10px] text-slate-400 truncate font-mono leading-tight">
                                            {m.user_id.slice(0, 8)}
                                        </span>
                                    </span>
                                </button>
                            );
                        })}
                    </div>
                )}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
