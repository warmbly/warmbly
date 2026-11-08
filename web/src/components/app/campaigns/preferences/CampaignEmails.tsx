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
import RampUpChoice from "@/components/app/campaigns/RampUpChoice";

type SetCampaign = React.Dispatch<React.SetStateAction<Campaign>>;

/** Which mailbox a lead's follow-ups leave from, with the tradeoff spelled out
 *  whenever follow-ups stop being pinned to one address. */
function FollowUpSenderSetting({
    newCampaign,
    setNewCampaign,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    const mode = newCampaign.follow_up_sender ?? "same";
    return (
        <div>
            <SettingRow
                title="Follow-ups"
                description="Which mailbox sends a lead's second and later emails."
                stack
                control={
                    <OptionSelect
                        aria-label="Follow-up sender"
                        cols={1}
                        value={mode}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, follow_up_sender: v }))}
                        options={[
                            {
                                value: "same",
                                label: "Same mailbox as the first email",
                                hint: "Recommended. One address per conversation; a follow-up waits for that mailbox when it is busy.",
                            },
                            {
                                value: "same_domain",
                                label: "Rotate within the same domain",
                                hint: "Each follow-up comes from another mailbox on the domain the lead already heard from.",
                            },
                            {
                                value: "rotate",
                                label: "Rotate every step",
                                hint: "Each follow-up is placed like a first email, from any mailbox in the campaign.",
                            },
                        ]}
                    />
                }
            />
            {mode !== "same" && (
                <div className="mt-2.5 rounded-md border border-amber-100 bg-amber-50/70 px-3 py-2.5 flex gap-2.5">
                    <AlertCircleIcon className="w-4 h-4 text-amber-600 shrink-0 mt-0.5" />
                    <div className="min-w-0 space-y-1">
                        <p className="text-[11.5px] text-amber-900/90 leading-relaxed">
                            Each follow-up can come from a different address, so a lead may see several senders in one
                            conversation. Threaded steps still reference every earlier email, which most mail apps use
                            to keep them together, but that is up to the recipient's app.
                        </p>
                        <p className="text-[10.5px] text-amber-800/70 leading-relaxed">
                            A reply goes to whichever mailbox sent the email being answered, so one lead's replies can
                            land in different inboxes. They are all still credited to the lead. To collect them in one
                            inbox, give the mailboxes the same reply-to address in their settings.
                        </p>
                    </div>
                </div>
            )}
        </div>
    );
}

/** Inbox rotation — distribution mode + how per-mailbox volume ramps up. */
export function RotationRampSection({
    newCampaign,
    setNewCampaign,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
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
                    {newCampaign.follow_up_sender === "same"
                        ? "Rotation picks the mailbox for a lead's first email, and every follow-up to that lead comes from the same address, so the conversation stays consistent. A lead only changes address if its mailbox stops being able to send for this campaign."
                        : "Rotation picks the mailbox for a lead's first email and, with the follow-up setting below, for every follow-up too."}{" "}
                    Each mailbox stays within its own daily limit and minimum gap.
                </p>
            </div>

            <FollowUpSenderSetting newCampaign={newCampaign} setNewCampaign={setNewCampaign} />

            {/* Ramp-up */}
            <SettingRow
                title="Ramp-up"
                description="How each mailbox's daily volume grows in this campaign. Separate from warmup, which keeps running alongside."
                stack
                control={
                    <RampUpChoice
                        value={{
                            warmup_graduation: newCampaign.warmup_graduation !== false,
                            ramp_enabled: newCampaign.ramp_enabled,
                            ramp_start: newCampaign.ramp_start,
                            ramp_increment: newCampaign.ramp_increment,
                            ramp_ceiling: newCampaign.ramp_ceiling,
                        }}
                        onChange={(p) => setNewCampaign((bef) => ({ ...bef, ...p }))}
                        level={newCampaign.ramp_level}
                    />
                }
            />
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
                campaignId={newCampaign.id}
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
                title="Keep running for new leads"
                description="Out of leads, the campaign stays active and waits instead of finishing, so leads from a linked segment, a form, the API or an automation keep getting the sequence. Linking a segment, a form or an automation to this campaign turns it on, and so does starting the campaign once every lead has finished."
                control={
                    <Toggle
                        id="campaign-pref-continuous"
                        value={newCampaign.continuous}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, continuous: v }))}
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
