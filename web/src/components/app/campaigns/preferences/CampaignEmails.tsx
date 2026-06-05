// Advanced campaign settings, split into the sections rendered on the
// single-scroll preferences page: rotation + ramp-up, ESP matching (with the
// visual coverage panel), lead flow, and tracking/headers. Each export returns
// ONLY its controls — the page's SettingsSection wrapper supplies the heading,
// icon and anchor. On-theme: slate/sky, rounded-md, 12.5px base, NumberInput
// for every number.

import { AlertCircleIcon } from "lucide-react";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { Label, NumberInput } from "@/components/ui/field";
import { EmailListInput, OptionSelect, SettingRow, Toggle } from "./components/CampaignPreferenceBoolBox";
import EspCoveragePanel from "./EspCoveragePanel";

type SetCampaign = React.Dispatch<React.SetStateAction<Campaign>>;

/** Rotation — distribution mode + per-mailbox daily ramp-up. */
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
            <SettingRow
                title="Rotation"
                description="How sends are distributed across the resolved mailboxes."
                stack
                control={
                    <OptionSelect
                        aria-label="Rotation mode"
                        cols={3}
                        value={newCampaign.rotation_mode}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, rotation_mode: v }))}
                        options={[
                            { value: "weighted", label: "Weighted", hint: "Favor mailboxes with more headroom" },
                            { value: "round_robin", label: "Round-robin", hint: "Cycle evenly through every mailbox" },
                            {
                                value: "least_recently_used",
                                label: "Least-recently-used",
                                hint: "Pick the mailbox idle the longest",
                            },
                        ]}
                    />
                }
            />

            <SettingRow
                title="Daily ramp-up"
                description="Gradually raise the per-mailbox send volume each day instead of starting at full limit."
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
                            <Label>Increment</Label>
                            <NumberInput
                                value={newCampaign.ramp_increment}
                                min={1}
                                max={100}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, ramp_increment: v }))}
                                suffix="+ / day"
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
                    {rampInvalid && (
                        <p className="flex items-center gap-1.5 text-[11px] text-rose-500">
                            <AlertCircleIcon className="w-3.5 h-3.5 shrink-0" />
                            Start must be less than or equal to the ceiling.
                        </p>
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
                <Label>New leads per day</Label>
                <NumberInput
                    value={newCampaign.max_new_leads_per_day}
                    min={0}
                    max={10000}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, max_new_leads_per_day: v }))}
                    suffix="leads / day"
                    className="w-48"
                />
                <p className="text-[11px] text-slate-400 mt-1.5">
                    {newCampaign.max_new_leads_per_day === 0
                        ? "0 = unlimited new leads per day."
                        : "Caps how many fresh leads enter the campaign each day."}
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

/** CC & BCC — extra recipients copied on every send. (Tracking domain is a
 *  per-mailbox setting and lives on the mailbox, not the campaign.) */
export function CcBccSection({
    newCampaign,
    setNewCampaign,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    return (
        <div className="space-y-6">
            <div>
                <Label>CC recipients</Label>
                <EmailListInput
                    values={newCampaign.cc}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, cc: v }))}
                />
                <p className="text-[11px] text-slate-400 mt-1.5">
                    A copy of each email goes to these addresses (visible to recipients).
                </p>
            </div>
            <div>
                <Label>BCC recipients</Label>
                <EmailListInput
                    values={newCampaign.bcc}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, bcc: v }))}
                />
                <p className="text-[11px] text-slate-400 mt-1.5">
                    A blind copy goes to these addresses (hidden from recipients).
                </p>
            </div>
        </div>
    );
}
