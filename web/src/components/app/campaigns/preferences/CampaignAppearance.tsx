// Standard campaign settings — the everyday controls (mirrors Instantly's
// "Standard" tab): identity, sending accounts, per-mailbox daily limit, and
// the deliverability toggles. On-theme: slate/sky, rounded-md, 12.5px base.

import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import TagSelector from "../../popup/select/TagSelector";
import AccountSelector from "./AccountSelector";
import { Segmented, SettingRow, Toggle } from "./components/CampaignPreferenceBoolBox";

const DAILY_MIN = 3;
const DAILY_MAX = 100;

export default function CampaignAppearance({
    campaign,
    newCampaign,
    setNewCampaign,
    explicitAccounts,
    setExplicitAccounts,
}: {
    campaign: Campaign;
    newCampaign: Campaign;
    setNewCampaign: React.Dispatch<React.SetStateAction<Campaign>>;
    // Explicit-strategy sender selection. Lives on the page (persisted via the
    // senders endpoint, not PATCH) and is seeded from the campaign's current pool.
    explicitAccounts: string[];
    setExplicitAccounts: React.Dispatch<React.SetStateAction<string[]>>;
}) {
    const dailyInvalid = newCampaign.daily_limit < DAILY_MIN || newCampaign.daily_limit > DAILY_MAX;

    return (
        <div className="space-y-7">
            {/* Identity */}
            <section className="space-y-4">
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
            </section>

            <hr className="border-slate-200/60" />

            {/* Sending */}
            <section className="space-y-4">
                <div>
                    <div className="flex items-center justify-between gap-3 mb-1.5">
                        <Label className="mb-0">Sending accounts</Label>
                        <Segmented
                            value={newCampaign.sender_strategy}
                            onChange={(v) =>
                                setNewCampaign((bef) => ({ ...bef, sender_strategy: v }))
                            }
                            options={[
                                { value: "tags", label: "By tag" },
                                { value: "explicit", label: "Specific accounts" },
                            ]}
                        />
                    </div>
                    {newCampaign.sender_strategy === "tags" ? (
                        <>
                            <TagSelector
                                selected={newCampaign.email_tags}
                                onAdd={(t) =>
                                    setNewCampaign((bef) => ({ ...bef, email_tags: [...bef.email_tags, t] }))
                                }
                                onRemove={(t) =>
                                    setNewCampaign((bef) => ({
                                        ...bef,
                                        email_tags: bef.email_tags.filter((e) => e !== t),
                                    }))
                                }
                            />
                            <p className="text-[11px] text-slate-400 mt-1.5">
                                Volume is split across every mailbox in these tags to protect deliverability.
                            </p>
                        </>
                    ) : (
                        <>
                            <AccountSelector value={explicitAccounts} onChange={setExplicitAccounts} />
                            <p className="text-[11px] text-slate-400 mt-1.5">
                                Send only from the mailboxes you pick here. Volume is split evenly across them.
                            </p>
                        </>
                    )}
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
            </section>

            <hr className="border-slate-200/60" />

            {/* Deliverability toggles */}
            <section className="space-y-5">
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
            </section>
        </div>
    );
}
