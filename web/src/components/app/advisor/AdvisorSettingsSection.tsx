// Advisor controls in workspace settings: switch it off, mute categories that
// are not relevant to how this workspace sends, or raise the bar so only
// serious findings surface.
//
// Muting is offered rather than resisted. A workspace that has decided it does
// not want copy advice will otherwise dismiss every copy card one at a time,
// which is the same outcome with more friction and a worse feedback signal.

import { useMemo } from "react";
import type { AdvisorCategory, AdvisorSeverity } from "@/lib/api/models/app/advisor/Advisor";
import { CATEGORY_LABEL, SEVERITY_LABEL } from "@/lib/api/models/app/advisor/Advisor";
import { Row, Section, ToggleRow } from "@/app/app/settings/_components/SectionShell";
import { useAdvisorSettings, useUpdateAdvisorSettings } from "@/lib/api/hooks/app/advisor/useAdvisor";

const CATEGORIES: { key: AdvisorCategory; description: string }[] = [
    { key: "deliverability", description: "Complaints, bounces, spam placement, and domain authentication" },
    { key: "mailbox", description: "Daily caps, send pacing, and mailbox health" },
    { key: "warmup", description: "Warmup coverage, volume, and pool standing" },
    { key: "campaign", description: "Sequence shape, reply rate, capacity, and scheduling" },
    { key: "copy", description: "Merge variables, length, subject lines, and bulk-mail phrasing" },
    { key: "list", description: "Shared inboxes, consumer domains, and suppressed contacts" },
];

const SEVERITIES: AdvisorSeverity[] = ["low", "medium", "high", "critical"];

export default function AdvisorSettingsSection({ canManage }: { canManage: boolean }) {
    const { data } = useAdvisorSettings();
    const update = useUpdateAdvisorSettings();

    const muted = useMemo(() => new Set(data?.muted_categories ?? []), [data]);
    const enabled = data?.enabled ?? true;
    const minSeverity = data?.min_severity ?? "low";
    const autopilot = data?.autopilot ?? false;

    // Every control sends the whole settings object: the endpoint replaces
    // rather than merges, so a partial payload would silently clear the rest.
    function save(
        patch: Partial<{
            enabled: boolean;
            muted_categories: string[];
            min_severity: AdvisorSeverity;
            autopilot: boolean;
        }>,
    ) {
        update.mutate({
            enabled,
            muted_categories: [...muted],
            muted_detectors: data?.muted_detectors ?? [],
            min_severity: minSeverity,
            autopilot,
            ...patch,
        });
    }

    function toggleCategory(key: AdvisorCategory, on: boolean) {
        const next = new Set(muted);
        if (on) next.delete(key);
        else next.add(key);
        save({ muted_categories: [...next] });
    }

    const disabled = !canManage || update.isPending;

    return (
        <Section
            eyebrow="Advisor"
            description="Warmbly checks your sending continuously and surfaces what to fix on the page where the fix lives. Detection runs on your own data with fixed thresholds; AI only rewrites the explanation, and never spends credits."
        >
            <ToggleRow
                label="Show recommendations"
                description="Turning this off hides every suggestion and clears the nav badges. Nothing is deleted, and turning it back on restores the current findings."
                checked={enabled}
                onChange={(v) => save({ enabled: v })}
                disabled={disabled}
            />

            {enabled ? (
                <>
                    <ToggleRow
                        label="Autopilot"
                        description="Applies the safe fixes on its own: lowering a cap that is above the safe band, widening a send gap, matching a campaign limit to what its mailboxes can carry, turning on one-click unsubscribe. It never pauses sending, edits your copy, or makes a change it cannot undo. Every fix runs with your permissions and appears in the audit log as you, and it stops if you leave the workspace."
                        checked={autopilot}
                        onChange={(v) => save({ autopilot: v })}
                        disabled={disabled}
                    />

                    <Row
                        label="Minimum severity"
                        description="Hide anything less urgent than this. Only critical and needs-attention findings ever badge a nav tab, whatever this is set to."
                    >
                        <div className="inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5">
                            {SEVERITIES.map((s) => (
                                <button
                                    key={s}
                                    type="button"
                                    disabled={disabled}
                                    onClick={() => save({ min_severity: s })}
                                    className={`h-6 rounded px-2 text-[12px] transition disabled:opacity-50 ${
                                        minSeverity === s
                                            ? "bg-white text-slate-900 shadow-sm font-medium"
                                            : "text-slate-500 hover:text-slate-700"
                                    }`}
                                >
                                    {SEVERITY_LABEL[s]}
                                </button>
                            ))}
                        </div>
                    </Row>

                    {CATEGORIES.map(({ key, description }) => (
                        <ToggleRow
                            key={key}
                            label={CATEGORY_LABEL[key]}
                            description={description}
                            checked={!muted.has(key)}
                            onChange={(v) => toggleCategory(key, v)}
                            disabled={disabled}
                        />
                    ))}
                </>
            ) : null}
        </Section>
    );
}
