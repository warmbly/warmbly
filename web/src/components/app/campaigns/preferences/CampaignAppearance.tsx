// Standard campaign settings, split into the sections rendered on the
// single-scroll preferences page: identity, sending accounts, and the
// deliverability toggles. Each export returns ONLY its controls — the page's
// SettingsSection wrapper supplies the heading, icon and anchor.
// On-theme: slate/sky, rounded-md, 12.5px base.

import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import SenderSelector from "./SenderSelector";
import { SettingRow, Toggle } from "./components/CampaignPreferenceBoolBox";

const DAILY_MIN = 3;
const DAILY_MAX = 100;

type SetCampaign = React.Dispatch<React.SetStateAction<Campaign>>;

/** General — campaign name + description. */
export function GeneralSection({
    campaign,
    newCampaign,
    setNewCampaign,
}: {
    campaign: Campaign;
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    return (
        <div className="space-y-4">
            <div>
                <Label>Campaign name</Label>
                <TextInput
                    value={newCampaign.name}
                    placeholder={campaign.name}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, name: v }))}
                    className="w-full max-w-[420px]"
                />
            </div>
            <div>
                <Label>Description</Label>
                <TextInput
                    value={newCampaign.description}
                    placeholder={campaign.description || "Optional — what this targets"}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, description: v }))}
                    className="w-full max-w-[420px]"
                />
            </div>
        </div>
    );
}

/** Sending accounts — the unified tag/mailbox picker + per-mailbox daily cap. */
export function SendingAccountsSection({
    newCampaign,
    setNewCampaign,
    explicitAccounts,
    setExplicitAccounts,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
    explicitAccounts: string[];
    setExplicitAccounts: React.Dispatch<React.SetStateAction<string[]>>;
}) {
    const dailyInvalid = newCampaign.daily_limit < DAILY_MIN || newCampaign.daily_limit > DAILY_MAX;
    return (
        <div className="space-y-4">
            <div>
                <Label>Sending accounts</Label>
                <SenderSelector
                    selectedTags={newCampaign.email_tags}
                    onTagsChange={(next) => setNewCampaign((bef) => ({ ...bef, email_tags: next }))}
                    selectedAccounts={explicitAccounts}
                    onAccountsChange={setExplicitAccounts}
                />
                <p className="text-[11px] text-slate-400 mt-1.5">
                    Pick tags, specific mailboxes, or both — volume is split evenly across the resolved pool.
                    Leave empty to send from every active mailbox.
                </p>
            </div>
            <div>
                <Label>Daily limit per mailbox</Label>
                <NumberInput
                    value={newCampaign.daily_limit}
                    min={DAILY_MIN}
                    max={DAILY_MAX}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, daily_limit: v }))}
                    suffix="emails / day"
                    className="w-48"
                />
                <p className={`text-[11px] mt-1.5 ${dailyInvalid ? "text-rose-500" : "text-slate-400"}`}>
                    {dailyInvalid
                        ? `Must be between ${DAILY_MIN} and ${DAILY_MAX}.`
                        : `${DAILY_MIN}–${DAILY_MAX}. Default 50 — stay conservative until reputation is proven.`}
                </p>
            </div>
        </div>
    );
}

/** Deliverability — the per-campaign send/tracking toggles. */
export function DeliverabilitySection({
    newCampaign,
    setNewCampaign,
}: {
    newCampaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    return (
        <div className="space-y-5">
            <SettingRow
                title="Stop on reply"
                description="Pause follow-ups for a contact once they respond."
                control={
                    <Toggle
                        id="campaign-pref-stop-on-reply"
                        value={newCampaign.stop_on_reply}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, stop_on_reply: v }))}
                    />
                }
            />
            <SettingRow
                title="Plain text only"
                description="Send as simple text for the best deliverability (disables tracking)."
                control={
                    <Toggle
                        id="campaign-pref-text"
                        value={newCampaign.text_only}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, text_only: v }))}
                    />
                }
            />
            <SettingRow
                title="Open tracking"
                description="Track email opens, but may slightly reduce deliverability."
                control={
                    <Toggle
                        id="campaign-pref-open-tracking"
                        value={newCampaign.open_tracking}
                        disabled={newCampaign.text_only}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, open_tracking: v }))}
                    />
                }
            />
            <SettingRow
                title="Link tracking"
                description="Track clicks on links to measure engagement (click-through rate)."
                control={
                    <Toggle
                        id="campaign-pref-link-tracking"
                        value={newCampaign.link_tracking}
                        disabled={newCampaign.text_only}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, link_tracking: v }))}
                    />
                }
            />
            <SettingRow
                title="Unsubscribe header"
                description="Add a List-Unsubscribe header for compliance and better deliverability."
                control={
                    <Toggle
                        id="campaign-pref-unsub"
                        value={newCampaign.unsubscribe_header}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, unsubscribe_header: v }))}
                    />
                }
            />
        </div>
    );
}
