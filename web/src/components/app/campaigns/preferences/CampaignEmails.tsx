// Advanced campaign settings, split into the sections rendered on the
// single-scroll preferences page: rotation + ramp-up, ESP matching (with the
// visual coverage panel), lead flow, and tracking/headers. Each export returns
// ONLY its controls — the page's SettingsSection wrapper supplies the heading,
// icon and anchor. On-theme: slate/sky, rounded-md, 12.5px base, NumberInput
// for every number.

import { useState } from "react";
import { AlertCircleIcon } from "lucide-react";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { Label, NumberInput } from "@/components/ui/field";
import { EmailListInput, OptionSelect, SettingRow, Toggle } from "./components/CampaignPreferenceBoolBox";
import EspCoveragePanel from "./EspCoveragePanel";

type SetCampaign = React.Dispatch<React.SetStateAction<Campaign>>;

/** RampPreview — a small day-by-day projection of the per-mailbox ramp curve
 *  (Day 1 = start, +increment each day, capped at the ceiling). Highlights the
 *  bar nearest today's live cap (ramp_level). */
function RampPreview({
    start,
    increment,
    ceiling,
    level,
}: {
    start: number;
    increment: number;
    ceiling: number;
    level: number;
}) {
    if (start <= 0 || ceiling <= 0 || start > ceiling) return null;
    const days: number[] = [];
    let v = start;
    let guard = 0;
    while (guard < 60) {
        const capped = Math.min(v, ceiling);
        days.push(capped);
        if (capped >= ceiling) break;
        v += Math.max(1, increment);
        guard++;
    }
    const MAX_BARS = 14;
    const shown = days.slice(0, MAX_BARS);
    const more = days.length - shown.length;
    const todayIdx = days.findIndex((d) => d >= level);

    return (
        <div>
            <div className="flex items-end gap-1 h-14">
                {shown.map((val, i) => {
                    const today = i === todayIdx;
                    return (
                        <div
                            key={i}
                            title={`Day ${i + 1}: ${val}/day`}
                            className={`flex-1 min-w-[4px] rounded-t-sm ${today ? "bg-sky-500" : "bg-sky-200"}`}
                            style={{ height: `${Math.max(10, (val / ceiling) * 100)}%` }}
                        />
                    );
                })}
            </div>
            <div className="flex items-center justify-between mt-1.5 text-[10px] text-slate-400">
                <span>
                    Day 1 · {start}/day
                </span>
                <span>
                    {more > 0 ? `+${more} more · ` : ""}Day {days.length} · {ceiling}/day, then steady
                </span>
            </div>
        </div>
    );
}

/** Inbox rotation — distribution mode + per-mailbox daily ramp-up. */
export function RotationRampSection({
    newCampaign,
    setNewCampaign,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    const rampInvalid = newCampaign.ramp_enabled && newCampaign.ramp_start > newCampaign.ramp_ceiling;
    return (
        <div className="space-y-5">
            {/* Inbox rotation */}
            <div>
                <SettingRow
                    title="Inbox rotation"
                    description="Send this campaign from several mailboxes so no single inbox sends too much — better deliverability and more total volume."
                    stack
                    control={
                        <OptionSelect
                            aria-label="Inbox rotation"
                            cols={1}
                            value={newCampaign.rotation_mode}
                            onChange={(v) => setNewCampaign((bef) => ({ ...bef, rotation_mode: v }))}
                            options={[
                                {
                                    value: "least_recently_used",
                                    label: "Even spacing",
                                    hint: "Recommended — always picks the mailbox that's been idle longest, for the most natural send pattern.",
                                },
                                {
                                    value: "round_robin",
                                    label: "Round-robin",
                                    hint: "Cycles through your mailboxes in order (A → B → C → A). A simple, even split.",
                                },
                                {
                                    value: "weighted",
                                    label: "Weighted",
                                    hint: "Sends more from your healthiest, higher-limit mailboxes.",
                                },
                            ]}
                        />
                    }
                />
                <p className="text-[11px] text-slate-400 mt-2 leading-relaxed">
                    Each mailbox stays within its own daily limit, and follow-ups always come from the mailbox that
                    sent the first email — so every thread stays consistent.
                </p>
            </div>

            {/* Daily ramp-up */}
            <SettingRow
                title="Daily ramp-up"
                description="Gradually raise each mailbox's daily volume instead of starting at full limit — a smooth growth curve protects deliverability. (Separate from warmup.)"
                control={
                    <Toggle
                        id="campaign-pref-ramp"
                        value={newCampaign.ramp_enabled}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, ramp_enabled: v }))}
                    />
                }
            />
            {newCampaign.ramp_enabled && (
                <div className="rounded-md border border-slate-200 bg-slate-50/40 p-3.5 space-y-3.5">
                    <div className="flex flex-wrap items-end gap-4">
                        <div>
                            <Label>Start</Label>
                            <NumberInput
                                value={newCampaign.ramp_start}
                                min={1}
                                max={500}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, ramp_start: v }))}
                                suffix="/ day"
                                className="w-36"
                            />
                        </div>
                        <div>
                            <Label>Increase by</Label>
                            <NumberInput
                                value={newCampaign.ramp_increment}
                                min={1}
                                max={100}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, ramp_increment: v }))}
                                suffix="/ day"
                                className="w-36"
                            />
                        </div>
                        <div>
                            <Label>Ceiling</Label>
                            <NumberInput
                                value={newCampaign.ramp_ceiling}
                                min={1}
                                max={500}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, ramp_ceiling: v }))}
                                suffix="/ day"
                                className="w-36"
                            />
                        </div>
                        <span className="inline-flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-sky-50 text-sky-700 text-[11.5px] font-medium">
                            Today's cap
                            <span className="font-mono tabular-nums">{newCampaign.ramp_level}</span>
                        </span>
                    </div>
                    {rampInvalid ? (
                        <p className="flex items-center gap-1.5 text-[11px] text-rose-500">
                            <AlertCircleIcon className="w-3.5 h-3.5 shrink-0" />
                            Start must be less than or equal to the ceiling.
                        </p>
                    ) : (
                        <RampPreview
                            start={newCampaign.ramp_start}
                            increment={newCampaign.ramp_increment}
                            ceiling={newCampaign.ramp_ceiling}
                            level={newCampaign.ramp_level}
                        />
                    )}
                </div>
            )}
        </div>
    );
}

/** ESP matching — the mode control + the visual coverage panel. */
export function EspMatchingSection({
    newCampaign,
    setNewCampaign,
    explicitAccounts,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
    explicitAccounts: string[];
}) {
    return (
        <div className="space-y-4">
            <SettingRow
                title="Provider matching"
                description="Match the sending mailbox provider to the recipient's provider (e.g. Google → Google)."
                stack
                control={
                    <OptionSelect
                        aria-label="ESP matching mode"
                        cols={3}
                        value={newCampaign.esp_match_mode}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, esp_match_mode: v }))}
                        options={[
                            { value: "off", label: "Off", hint: "Ignore provider when choosing a mailbox" },
                            { value: "prefer", label: "Prefer same", hint: "Use a same-provider mailbox when free" },
                            { value: "strict", label: "Strict same", hint: "Only send from a same-provider mailbox" },
                        ]}
                    />
                }
            />
            <EspCoveragePanel
                mode={newCampaign.esp_match_mode}
                emailTags={newCampaign.email_tags}
                explicitAccounts={explicitAccounts}
            />
        </div>
    );
}

/** Lead flow — new-lead throttle, prioritization, and risky-address policy. */
export function LeadFlowSection({
    newCampaign,
    setNewCampaign,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    return (
        <div className="space-y-5">
            <div>
                <Label>New leads contacted per day</Label>
                <NumberInput
                    value={newCampaign.max_new_leads_per_day}
                    min={0}
                    max={10000}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, max_new_leads_per_day: v }))}
                    suffix={newCampaign.max_new_leads_per_day === 0 ? "= no limit" : "leads / day"}
                    className="w-48"
                />
                <p className="text-[11px] text-slate-400 mt-1.5 leading-relaxed">
                    Limits how many brand-new leads get their <span className="text-slate-500">very first</span>{" "}
                    email from this campaign each day, so a fresh list goes out as a steady trickle instead of one
                    big blast. Follow-ups to people already in the campaign still send and don't count against this.
                </p>
            </div>
            <SettingRow
                title="Prioritize new leads"
                description="Send to newly added leads before resuming the existing queue."
                control={
                    <Toggle
                        id="campaign-pref-prioritize-new"
                        value={newCampaign.prioritize_new_leads}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, prioritize_new_leads: v }))}
                    />
                }
            />
            <SettingRow
                title="Send to risky emails"
                description="Attempt delivery to addresses flagged risky by verification (may increase bounces)."
                control={
                    <Toggle
                        id="campaign-pref-risk"
                        value={newCampaign.risky_emails}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, risky_emails: v }))}
                    />
                }
            />
        </div>
    );
}

/** CC & BCC — extra recipients copied on every send. Gmail-style: the fields
 *  stay hidden behind "Add CC / Add BCC" until needed, so the section is calm
 *  by default. (Tracking domain is a per-mailbox setting and lives on the
 *  mailbox, not the campaign.) */
export function CcBccSection({
    newCampaign,
    setNewCampaign,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    const [showCc, setShowCc] = useState(newCampaign.cc.length > 0);
    const [showBcc, setShowBcc] = useState(newCampaign.bcc.length > 0);

    const addBtn =
        "inline-flex items-center h-7 px-2.5 rounded-md border border-dashed border-slate-300 text-slate-500 hover:border-slate-400 hover:text-slate-700 text-[12px] font-medium transition-colors";

    return (
        <div className="space-y-4">
            {showCc && (
                <div>
                    <div className="flex items-center justify-between mb-1.5">
                        <Label className="mb-0">CC</Label>
                        <button
                            type="button"
                            onClick={() => {
                                setShowCc(false);
                                setNewCampaign((bef) => ({ ...bef, cc: [] }));
                            }}
                            className="text-[11px] text-slate-400 hover:text-rose-500 transition-colors"
                        >
                            Remove
                        </button>
                    </div>
                    <EmailListInput
                        values={newCampaign.cc}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, cc: v }))}
                    />
                    <p className="text-[11px] text-slate-400 mt-1.5">
                        Visible to recipients. Paste or type several — separate with a comma, space, or Enter.
                    </p>
                </div>
            )}

            {showBcc && (
                <div>
                    <div className="flex items-center justify-between mb-1.5">
                        <Label className="mb-0">BCC</Label>
                        <button
                            type="button"
                            onClick={() => {
                                setShowBcc(false);
                                setNewCampaign((bef) => ({ ...bef, bcc: [] }));
                            }}
                            className="text-[11px] text-slate-400 hover:text-rose-500 transition-colors"
                        >
                            Remove
                        </button>
                    </div>
                    <EmailListInput
                        values={newCampaign.bcc}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, bcc: v }))}
                    />
                    <p className="text-[11px] text-slate-400 mt-1.5">Hidden from recipients.</p>
                </div>
            )}

            {(!showCc || !showBcc) && (
                <div className="flex flex-wrap items-center gap-2">
                    {!showCc && (
                        <button type="button" onClick={() => setShowCc(true)} className={addBtn}>
                            + Add CC
                        </button>
                    )}
                    {!showBcc && (
                        <button type="button" onClick={() => setShowBcc(true)} className={addBtn}>
                            + Add BCC
                        </button>
                    )}
                    {!showCc && !showBcc && (
                        <span className="text-[11px] text-slate-400">
                            Optionally copy extra addresses on every email this campaign sends.
                        </span>
                    )}
                </div>
            )}
        </div>
    );
}
