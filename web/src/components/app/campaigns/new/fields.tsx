// Small building blocks the new-campaign steps share.

import React from "react";
import type { LucideIcon } from "lucide-react";
import { Label } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { TimePicker } from "@/components/ui/TimePicker";
import WeekdayBitmask from "@/components/app/campaigns/schedule/WeekdayBitmask";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import { useUserProfile } from "@/hooks/context/user";
import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import { followWorkspaceLabel, timezoneOptions } from "@/lib/timezone";
import { cn } from "@/lib/utils";
import { EVERY_DAY_MASK, WEEKDAYS, WEEKDAYS_MASK, daysLabel, fmt12, type Draft } from "./draft";

export type Patch = (p: Partial<Draft> | ((draft: Draft) => Partial<Draft>)) => void;

export function StepIntro({ title, hint, children }: { title: string; hint?: string; children?: React.ReactNode }) {
    return (
        <div className="mb-5 flex items-start gap-3">
            <div className="min-w-0 flex-1">
                <p className="text-[15px] text-slate-900 font-semibold tracking-[-0.01em]">{title}</p>
                {hint && <p className="text-[12px] text-slate-500 mt-1 leading-relaxed">{hint}</p>}
            </div>
            {children}
        </div>
    );
}

export function SectionLabel({ children, right }: { children: React.ReactNode; right?: React.ReactNode }) {
    return (
        <div className="flex items-center justify-between mb-2">
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{children}</span>
            {right}
        </div>
    );
}

export function ChoiceCard({
    selected,
    icon: Icon,
    title,
    description,
    onSelect,
}: {
    selected: boolean;
    icon: LucideIcon;
    title: string;
    description: string;
    onSelect: () => void;
}) {
    return (
        <button
            type="button"
            role="radio"
            aria-checked={selected}
            onClick={onSelect}
            className={cn(
                "text-left rounded-md border px-3 py-2.5 flex items-start gap-2.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                selected
                    ? "border-sky-400 bg-sky-50/60 ring-1 ring-inset ring-sky-400"
                    : "border-slate-200 hover:border-slate-300 hover:bg-slate-50",
            )}
        >
            <span
                className={cn(
                    "size-6 rounded-md inline-flex items-center justify-center shrink-0 mt-0.5 transition-colors",
                    selected ? "bg-sky-600 text-white" : "bg-slate-100 text-slate-600",
                )}
            >
                <Icon className="w-3.5 h-3.5" />
            </span>
            <span className="min-w-0">
                <span className="block text-[12.5px] text-slate-900 font-medium">{title}</span>
                <span className="block text-[11px] text-slate-500 mt-0.5 leading-relaxed">{description}</span>
            </span>
        </button>
    );
}

export function TimezoneField({ draft, patch }: { draft: Draft; patch: Patch }) {
    const profile = useUserProfile();
    const org = useCurrentOrganization();
    const options = React.useMemo<SelectOption[]>(
        () => [
            { value: "", label: followWorkspaceLabel(org.data?.timezone) },
            ...timezoneOptions(profile?.timezones, draft.timezone),
        ],
        [profile?.timezones, draft.timezone, org.data?.timezone],
    );
    return (
        <div>
            <Label>Timezone</Label>
            <SelectMenu
                value={draft.timezone}
                onChange={(v) => patch({ timezone: v })}
                options={options}
                fullWidth
                placeholder="Select a timezone"
                aria-label="Sending timezone"
            />
        </div>
    );
}

export function SendingWindowFields({ draft, patch }: { draft: Draft; patch: Patch }) {
    const windowInvalid = draft.startTime >= draft.endTime;
    const preset = (mask: number, label: string) => (
        <button
            type="button"
            onClick={() => patch({ days: mask })}
            className={cn(
                "px-1.5 h-5 rounded transition-colors",
                draft.days === mask ? "bg-sky-50 text-sky-700" : "text-slate-400 hover:text-slate-700 hover:bg-slate-100",
            )}
        >
            {label}
        </button>
    );
    return (
        <div className="space-y-4">
            <div>
                <div className="flex items-baseline justify-between">
                    <Label>Sending days</Label>
                    <div className="flex items-center gap-1 text-[10.5px]">
                        {preset(WEEKDAYS_MASK, "Weekdays")}
                        {preset(EVERY_DAY_MASK, "Every day")}
                    </div>
                </div>
                <div className="mt-1">
                    <WeekdayBitmask weekdays={WEEKDAYS} value={draft.days} setValue={(v) => patch({ days: v })} />
                </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
                <div>
                    <Label>From</Label>
                    <TimePicker
                        value={draft.startTime}
                        onChange={(v) => patch({ startTime: v })}
                        stepMinutes={30}
                        fullWidth
                        placeholder="Start"
                    />
                </div>
                <div>
                    <Label>Until</Label>
                    <TimePicker
                        value={draft.endTime}
                        onChange={(v) => patch({ endTime: v })}
                        stepMinutes={30}
                        fullWidth
                        placeholder="End"
                    />
                </div>
            </div>
            <p className={cn("text-[11.5px] leading-relaxed", windowInvalid ? "text-amber-700" : "text-slate-500")}>
                {windowInvalid
                    ? "The window ends before it starts. Pick an end time after the start time."
                    : `${daysLabel(draft.days)}, ${fmt12(draft.startTime)} to ${fmt12(draft.endTime)}. Sends spread out across the window.`}
            </p>
        </div>
    );
}

export function SwitchRow({
    label,
    description,
    value,
    onChange,
}: {
    label: string;
    description: string;
    value: boolean;
    onChange: (v: boolean) => void;
}) {
    return (
        // The row is the click target; the switch stops propagation so a click
        // on it does not toggle twice.
        <div
            onClick={() => onChange(!value)}
            className="w-full px-3 py-2.5 flex items-start justify-between gap-4 cursor-pointer select-none hover:bg-slate-50 transition-colors"
        >
            <div className="min-w-0">
                <p className="text-[12.5px] text-slate-900 font-medium">{label}</p>
                <p className="text-[11px] text-slate-500 mt-0.5 leading-relaxed">{description}</p>
            </div>
            <span onClick={(e) => e.stopPropagation()} className="shrink-0 mt-0.5 inline-flex">
                <Toggle value={value} onChange={onChange} />
            </span>
        </div>
    );
}
