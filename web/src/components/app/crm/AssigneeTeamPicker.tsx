// A themed dropdown for choosing who a task is assigned to: a default
// (workspace/campaign owner), a whole team, or a single member. Emits both ids
// (one set, the other null) so callers can store task_assigned_to /
// task_assigned_team_id independently. Teams render with their color dot.

import React from "react";
import { ChevronDownIcon, UserIcon, UsersRoundIcon } from "lucide-react";
import useMembers from "@/lib/api/hooks/app/organizations/useMembers";
import useTeams from "@/lib/api/hooks/app/teams/useTeams";
import useClickOutside from "@/hooks/useClickOutside";
import { cn } from "@/lib/utils";

export interface AssigneeValue {
    userId?: string | null;
    teamId?: string | null;
}

export default function AssigneeTeamPicker({
    value,
    onChange,
    fallbackLabel = "Workspace owner",
    className,
}: {
    value: AssigneeValue;
    onChange: (v: AssigneeValue) => void;
    fallbackLabel?: string;
    className?: string;
}) {
    const { data: members } = useMembers();
    const { data: teams } = useTeams();
    const [open, setOpen] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));

    const memberList = members ?? [];
    const teamList = teams ?? [];
    const selectedMember = value.userId ? memberList.find((m) => m.user_id === value.userId) : undefined;
    const selectedTeam = value.teamId ? teamList.find((t) => t.id === value.teamId) : undefined;

    const pick = (v: AssigneeValue) => {
        onChange(v);
        setOpen(false);
    };

    const trigger = selectedTeam ? (
        <>
            <span className="size-2.5 shrink-0 rounded-full" style={{ background: selectedTeam.color }} />
            <span className="flex-1 truncate text-left">{selectedTeam.name}</span>
        </>
    ) : selectedMember ? (
        <>
            <UserIcon className="w-3.5 h-3.5 shrink-0 text-slate-400" />
            <span className="flex-1 truncate text-left">{selectedMember.email || selectedMember.name || "Member"}</span>
        </>
    ) : (
        <>
            <UserIcon className="w-3.5 h-3.5 shrink-0 text-slate-400" />
            <span className="flex-1 truncate text-left text-slate-500">{fallbackLabel}</span>
        </>
    );

    return (
        <div ref={ref} className={cn("relative", className)}>
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="flex h-7 w-full items-center gap-1.5 rounded-md border border-slate-200 bg-white px-2.5 text-[12px] text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900"
            >
                {trigger}
                <ChevronDownIcon className="w-3 h-3 shrink-0 text-slate-400" />
            </button>
            {open && (
                <div className="absolute right-0 md:right-auto md:left-0 top-full z-30 mt-1 max-h-64 w-full min-w-[15rem] max-w-[calc(100vw-2rem)] overflow-y-auto rounded-md border border-slate-200 bg-white py-1 shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)]">
                    <button
                        type="button"
                        onClick={() => pick({ userId: null, teamId: null })}
                        className={cn(
                            "flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] transition-colors hover:bg-slate-100",
                            !value.userId && !value.teamId ? "font-medium text-slate-900" : "text-slate-700",
                        )}
                    >
                        <UserIcon className="w-3.5 h-3.5 text-slate-400" /> {fallbackLabel}
                    </button>
                    {teamList.length > 0 && (
                        <>
                            <div className="px-2.5 pt-2 pb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Teams</div>
                            {teamList.map((t) => (
                                <button
                                    key={t.id}
                                    type="button"
                                    onClick={() => pick({ teamId: t.id, userId: null })}
                                    className={cn(
                                        "flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] transition-colors hover:bg-slate-100",
                                        value.teamId === t.id ? "font-medium text-slate-900" : "text-slate-700",
                                    )}
                                >
                                    <span className="size-2.5 shrink-0 rounded-full" style={{ background: t.color }} />
                                    <span className="flex-1 truncate">{t.name}</span>
                                    <UsersRoundIcon className="w-3 h-3 text-slate-300" />
                                </button>
                            ))}
                        </>
                    )}
                    {memberList.length > 0 && (
                        <>
                            <div className="px-2.5 pt-2 pb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Members</div>
                            {memberList.map((m) => (
                                <button
                                    key={m.id}
                                    type="button"
                                    onClick={() => pick({ userId: m.user_id, teamId: null })}
                                    className={cn(
                                        "flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-[12px] transition-colors hover:bg-slate-100",
                                        value.userId === m.user_id ? "font-medium text-slate-900" : "text-slate-700",
                                    )}
                                >
                                    <UserIcon className="w-3.5 h-3.5 shrink-0 text-slate-400" />
                                    <span className="flex-1 truncate">{m.email || m.name || "Member"}</span>
                                    <span className="text-[10px] capitalize text-slate-400">{m.role}</span>
                                </button>
                            ))}
                        </>
                    )}
                </div>
            )}
        </div>
    );
}
