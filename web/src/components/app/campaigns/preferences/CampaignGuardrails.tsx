// Auto-pause guardrails — the campaign stops itself when its engagement rates
// leave the band you set, instead of waiting for a mailbox provider to react.
//
// The numbers here are percentages. Bounce and complaint rates are ceilings
// (pause at or above); the reply rate is a floor (pause below), because a
// campaign at volume that nobody answers is spending sender reputation for
// nothing. Setting a rate to 0 turns that rule off.

import { PauseCircleIcon } from "lucide-react";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { Label, NumberInput } from "@/components/ui/field";
import { SettingRow, Toggle } from "./components/CampaignPreferenceBoolBox";

type SetCampaign = React.Dispatch<React.SetStateAction<Campaign>>;

// Provider guidance behind the defaults, shown so the numbers are not magic.
const REFERENCE = [
    "Google asks senders to keep spam complaints under 0.10% and never reach 0.30%.",
    "Amazon SES puts an account under review at a 5% bounce rate and can pause sending at 10%.",
];

export function GuardrailsSection({
    newCampaign,
    setNewCampaign,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    const trippedAt = newCampaign.guardrail_tripped_at;
    const paused = newCampaign.status === "paused_guardrail";

    return (
        <div className="space-y-5">
            {(paused || trippedAt) && (
                <div className="rounded-md border border-amber-100 bg-amber-50/70 px-3 py-2.5 flex gap-2.5">
                    <PauseCircleIcon className="w-4 h-4 text-amber-600 shrink-0 mt-0.5" />
                    <div className="min-w-0">
                        <p className="text-[11.5px] text-amber-900/90 leading-relaxed">
                            {newCampaign.guardrail_reason || "This campaign was paused automatically by a guardrail."}
                        </p>
                        {trippedAt && (
                            <p className="text-[10.5px] text-amber-800/70 mt-1">
                                {new Date(trippedAt).toLocaleString()}
                            </p>
                        )}
                        {paused && (
                            <p className="text-[10.5px] text-amber-800/70 mt-1">
                                Starting the campaign again clears this. Fix the underlying problem first, or it will
                                trip again on the next check.
                            </p>
                        )}
                    </div>
                </div>
            )}

            <SettingRow
                title="Auto-pause"
                description="Stop this campaign the moment its bounce, complaint, or reply rate leaves the band below. Checked every 15 minutes."
                control={
                    <Toggle
                        id="campaign-pref-guardrail"
                        value={newCampaign.guardrail_enabled}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, guardrail_enabled: v }))}
                    />
                }
            />

            {newCampaign.guardrail_enabled && (
                <div className="rounded-md border border-slate-200 bg-slate-50/40 p-3.5 space-y-4">
                    <div className="flex flex-wrap items-end gap-4">
                        <div>
                            <Label>Pause above bounce rate</Label>
                            <NumberInput
                                value={newCampaign.guardrail_bounce_rate_max}
                                min={0}
                                max={100}
                                step={0.5}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, guardrail_bounce_rate_max: v }))}
                                suffix="%"
                                className="w-36"
                            />
                        </div>
                        <div>
                            <Label>Pause above complaint rate</Label>
                            <NumberInput
                                value={newCampaign.guardrail_complaint_rate_max}
                                min={0}
                                max={100}
                                step={0.05}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, guardrail_complaint_rate_max: v }))}
                                suffix="%"
                                className="w-36"
                            />
                        </div>
                        <div>
                            <Label>Pause below reply rate</Label>
                            <NumberInput
                                value={newCampaign.guardrail_reply_rate_min}
                                min={0}
                                max={100}
                                step={0.5}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, guardrail_reply_rate_min: v }))}
                                suffix="%"
                                className="w-36"
                            />
                        </div>
                    </div>

                    <div className="flex flex-wrap items-end gap-4">
                        <div>
                            <Label>Only after</Label>
                            <NumberInput
                                value={newCampaign.guardrail_min_sample}
                                min={1}
                                max={100000}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, guardrail_min_sample: v }))}
                                suffix="sends"
                                className="w-40"
                            />
                        </div>
                        <div>
                            <Label>Measured over the last</Label>
                            <NumberInput
                                value={newCampaign.guardrail_window_days}
                                min={0}
                                max={365}
                                onChange={(v) => setNewCampaign((bef) => ({ ...bef, guardrail_window_days: v }))}
                                suffix="days"
                                className="w-40"
                            />
                        </div>
                    </div>

                    <div className="space-y-1.5 pt-0.5">
                        <p className="text-[11px] text-slate-500 leading-relaxed">
                            A rate of <b>0</b> turns that rule off. The reply-rate floor is off by default: pausing for
                            weak engagement is a deliberate choice. A window of <b>0</b> days measures the campaign&apos;s
                            whole history.
                        </p>
                        {REFERENCE.map((line) => (
                            <p key={line} className="text-[10.5px] text-slate-400 leading-relaxed">
                                {line}
                            </p>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
}
