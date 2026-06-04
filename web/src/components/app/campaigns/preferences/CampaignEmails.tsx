// Advanced campaign settings — the Instantly-level send controls: sender
// strategy + rotation, per-campaign ramp-up, ESP matching, new-lead throttle,
// bounce protection, a campaign tracking-domain override, and cc/bcc.
// On-theme: slate/sky, rounded-md, 12.5px base, NumberInput for every number.

import {
    CheckCircle2Icon,
    AlertCircleIcon,
    Loader2Icon,
} from "lucide-react";
import toast from "react-hot-toast";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import { EmailListInput, Segmented, SettingRow, Toggle } from "./components/CampaignPreferenceBoolBox";
import useVerifyCampaignTrackingDomain from "@/lib/api/hooks/app/campaigns/useVerifyCampaignTrackingDomain";

export default function CampaignEmails({
    campaign,
    newCampaign,
    setNewCampaign,
}: {
    campaign: Campaign;
    newCampaign: Campaign;
    setNewCampaign: React.Dispatch<React.SetStateAction<Campaign>>;
}) {
    const verify = useVerifyCampaignTrackingDomain(campaign.id);
    // Verify resolves the CNAME for the SAVED domain, so it's only offered when
    // the field has no unsaved edits.
    const trackingDirty = newCampaign.tracking_domain !== campaign.tracking_domain;
    const rampInvalid =
        newCampaign.ramp_enabled && newCampaign.ramp_start > newCampaign.ramp_ceiling;

    return (
        <div className="space-y-7">
            {/* Rotation — sender selection (tags vs specific accounts) now lives in
                the Standard tab next to the sending-accounts picker. */}
            <section className="space-y-4">
                <SettingRow
                    title="Rotation"
                    description="How sends are distributed across the resolved mailboxes."
                    control={
                        <Segmented
                            value={newCampaign.rotation_mode}
                            onChange={(v) => setNewCampaign((bef) => ({ ...bef, rotation_mode: v }))}
                            options={[
                                { value: "weighted", label: "Weighted" },
                                { value: "round_robin", label: "Round-robin" },
                                { value: "least_recently_used", label: "Least-recently-used" },
                            ]}
                        />
                    }
                />
            </section>

            <hr className="border-slate-200/60" />

            {/* Ramp-up */}
            <section className="space-y-4">
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
                                    onChange={(v) =>
                                        setNewCampaign((bef) => ({ ...bef, ramp_increment: v }))
                                    }
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
                                    onChange={(v) =>
                                        setNewCampaign((bef) => ({ ...bef, ramp_ceiling: v }))
                                    }
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
            </section>

            <hr className="border-slate-200/60" />

            {/* ESP matching + new-lead throttle */}
            <section className="space-y-4">
                <SettingRow
                    title="ESP matching"
                    description="Match the sending mailbox provider to the recipient's provider (e.g. Google → Google)."
                    control={
                        <Segmented
                            value={newCampaign.esp_match_mode}
                            onChange={(v) => setNewCampaign((bef) => ({ ...bef, esp_match_mode: v }))}
                            options={[
                                { value: "off", label: "Off" },
                                { value: "prefer", label: "Prefer same" },
                                { value: "strict", label: "Strict same" },
                            ]}
                        />
                    }
                />
                {newCampaign.esp_match_mode === "strict" && (
                    <p className="text-[11px] text-slate-400 -mt-1">
                        Strict matching can slow sending when no same-provider mailbox is free.
                    </p>
                )}
                <div>
                    <Label>New leads per day</Label>
                    <NumberInput
                        value={newCampaign.max_new_leads_per_day}
                        min={0}
                        max={10000}
                        onChange={(v) =>
                            setNewCampaign((bef) => ({ ...bef, max_new_leads_per_day: v }))
                        }
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
                            onChange={(v) =>
                                setNewCampaign((bef) => ({ ...bef, prioritize_new_leads: v }))
                            }
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
            </section>

            <hr className="border-slate-200/60" />

            {/* Tracking domain */}
            <section className="space-y-2">
                <div className="flex items-center gap-2">
                    <Label className="mb-0">Tracking domain</Label>
                    {newCampaign.tracking_domain ? (
                        newCampaign.tracking_domain_verified ? (
                            <span className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-emerald-50 text-emerald-700 text-[10px] uppercase tracking-[0.1em] font-medium">
                                <CheckCircle2Icon className="w-3 h-3" />
                                Verified
                            </span>
                        ) : (
                            <span className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-amber-50 text-amber-700 text-[10px] uppercase tracking-[0.1em] font-medium">
                                <AlertCircleIcon className="w-3 h-3" />
                                Unverified
                            </span>
                        )
                    ) : null}
                    {campaign.tracking_domain && !campaign.tracking_domain_verified && !trackingDirty && (
                        <button
                            type="button"
                            disabled={verify.isPending}
                            onClick={() =>
                                verify.mutate(undefined, {
                                    onSuccess: (s) =>
                                        s.tracking_domain_verified
                                            ? toast.success("Tracking domain verified")
                                            : toast.error("CNAME not found yet — check the record and retry"),
                                    onError: () => toast.error("Couldn't verify tracking domain"),
                                })
                            }
                            className="inline-flex items-center gap-1 h-5 px-1.5 rounded bg-sky-600 hover:bg-sky-700 text-white text-[10px] uppercase tracking-[0.1em] font-medium transition-colors disabled:opacity-60"
                        >
                            {verify.isPending && <Loader2Icon className="w-3 h-3 animate-spin" />}
                            {verify.isPending ? "Verifying" : "Verify"}
                        </button>
                    )}
                    {campaign.tracking_domain && !campaign.tracking_domain_verified && trackingDirty && (
                        <span className="text-[10px] text-slate-400">Save to verify</span>
                    )}
                </div>
                <TextInput
                    value={newCampaign.tracking_domain}
                    placeholder="track.yourdomain.com"
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, tracking_domain: v }))}
                    className="w-full max-w-[420px]"
                />
                <p className="text-[11px] text-slate-400">
                    Add a CNAME from this host to our tracking endpoint, then verify. Tracking links
                    only use this domain once it's verified.
                </p>
            </section>

            <hr className="border-slate-200/60" />

            {/* CC / BCC */}
            <section className="space-y-5">
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
            </section>
        </div>
    );
}
