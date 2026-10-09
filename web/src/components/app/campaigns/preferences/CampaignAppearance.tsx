// Standard campaign settings, split into the sections rendered on the
// single-scroll preferences page: identity, sending accounts, and the
// deliverability toggles. Each export returns ONLY its controls — the page's
// SettingsSection wrapper supplies the heading, icon and anchor.
// On-theme: slate/sky, rounded-md, 12.5px base.

import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { isValidCampaignDailyLimit } from "@/lib/api/models/app/campaigns/Campaign";
import { Label, NumberInput, TextInput } from "@/components/ui/field";
import SenderSelector from "./SenderSelector";
import { SettingRow, Toggle } from "./components/CampaignPreferenceBoolBox";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { useOutreachSettings } from "@/lib/api/hooks/app/outreach/useOutreachSettings";

const UNSUB_MODES: SelectOption[] = [
    { value: "inherit", label: "Workspace default" },
    { value: "text", label: "Reply to opt out (text line)" },
    { value: "link", label: "Unsubscribe link" },
    { value: "off", label: "Nothing" },
];

const DAILY_MIN = 3;
const DAILY_MAX = 5000;

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
    savedDailyLimit,
    setNewCampaign,
    explicitAccounts,
    setExplicitAccounts,
}: {
    newCampaign: Campaign;
    savedDailyLimit?: number;
    setNewCampaign: SetCampaign;
    explicitAccounts: string[];
    setExplicitAccounts: React.Dispatch<React.SetStateAction<string[]>>;
}) {
    const dailyInvalid = !isValidCampaignDailyLimit(newCampaign.daily_limit, savedDailyLimit);
    const dailyLegacy = !dailyInvalid && newCampaign.daily_limit <= 0;
    const dailyHigh = !dailyInvalid && newCampaign.daily_limit > 100;
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
                <Label>Maximum per mailbox per day</Label>
                <NumberInput
                    value={newCampaign.daily_limit}
                    min={DAILY_MIN}
                    max={DAILY_MAX}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, daily_limit: v }))}
                    suffix="emails / day"
                    className="w-48"
                />
                <p className={`text-[11px] mt-1.5 ${dailyInvalid ? "text-rose-500" : dailyHigh ? "text-amber-600" : "text-slate-400"}`}>
                    {dailyInvalid
                        ? `Must be between ${DAILY_MIN} and ${DAILY_MAX}.`
                        : dailyLegacy
                          ? "Saved without a campaign-specific cap. Mailbox and sending-plan limits still apply. Enter 3–5000 to set a maximum."
                          : dailyHigh
                            ? "Well above the 30–50/day safe cold-outreach band. Every mailbox in the pool needs the reputation and provider capacity to carry this."
                            : `Configurable ${DAILY_MIN}–${DAILY_MAX}. This campaign's ceiling, not a daily target. New mailboxes plan 28–32/day; tune the range in each mailbox's Sending tab.`}
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
    // The warning below is only true when the opt-out this campaign actually
    // sends is a link, so "inherit" has to be resolved against the workspace
    // default rather than assumed.
    const { data: outreach } = useOutreachSettings();
    const mode = newCampaign.unsubscribe_mode ?? "inherit";
    const effectiveMode = mode === "inherit" || !mode ? (outreach?.unsubscribe?.mode ?? "text") : mode;
    const plainTextLinkOptOut = newCampaign.text_only && effectiveMode === "link";

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
                description="Track clicks on links to measure engagement (click-through rate). Each link is tracked on its own, so a contact's activity shows exactly which link was clicked."
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
                title="UTM parameters"
                description="Tag every link with utm_source, utm_medium, utm_campaign and utm_content (the link's own text) so clicks show up attributed in your web analytics. Links that already carry a UTM value keep it."
                control={
                    <Toggle
                        id="campaign-pref-utm-tracking"
                        value={newCampaign.utm_tracking}
                        disabled={newCampaign.text_only}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, utm_tracking: v }))}
                    />
                }
            />
            {newCampaign.utm_tracking && !newCampaign.text_only && (
                <UTMFields campaign={newCampaign} setNewCampaign={setNewCampaign} />
            )}
            <SettingRow
                title="Unsubscribe header"
                description="Add a List-Unsubscribe header so mail clients can show their own one-click unsubscribe. It is a header, not visible copy, so it changes nothing about how the email reads and works on plain-text sends too. This is the opt-out Gmail and Yahoo look for; leave it on."
                control={
                    <Toggle
                        id="campaign-pref-unsub"
                        value={newCampaign.unsubscribe_header}
                        onChange={(v) => setNewCampaign((bef) => ({ ...bef, unsubscribe_header: v }))}
                    />
                }
            />
            <SettingRow
                title="Opt-out line"
                description={
                    <>
                        The opt-out appended after the signature of every email in this campaign. Reply to opt out reads
                        as a personal email and is honoured automatically; a link is for lists that need one, and the
                        header above already covers the bulk-sender rules. The workspace default is set under Settings
                        &gt; Sending.
                        {plainTextLinkOptOut && (
                            <span className="mt-1 block text-amber-700">
                                This campaign sends plain text only, where a link has nowhere to hide its address: the
                                recipient reads the full unsubscribe URL. Prefer the header and the reply line here.
                            </span>
                        )}
                    </>
                }
                control={
                    <SelectMenu
                        value={newCampaign.unsubscribe_mode ?? "inherit"}
                        onChange={(v) =>
                            setNewCampaign((bef) => ({ ...bef, unsubscribe_mode: v as Campaign["unsubscribe_mode"] }))
                        }
                        options={UNSUB_MODES}
                        aria-label="Opt-out line"
                        minWidth={240}
                        align="end"
                    />
                }
            />
        </div>
    );
}

/** The three campaign-level UTM values; utm_content is per link and not editable. */
export function UTMFields({
    campaign,
    setNewCampaign,
}: {
    campaign: Campaign;
    setNewCampaign: SetCampaign;
}) {
    return (
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 pl-0 sm:pl-4 sm:border-l-2 sm:border-slate-100">
            <div>
                <Label>utm_source</Label>
                <TextInput
                    value={campaign.utm_source}
                    placeholder="warmbly"
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, utm_source: v }))}
                    className="w-full"
                />
            </div>
            <div>
                <Label>utm_medium</Label>
                <TextInput
                    value={campaign.utm_medium}
                    placeholder="email"
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, utm_medium: v }))}
                    className="w-full"
                />
            </div>
            <div>
                <Label>utm_campaign</Label>
                <TextInput
                    value={campaign.utm_campaign}
                    placeholder={utmSlug(campaign.name) || "campaign"}
                    onChange={(v) => setNewCampaign((bef) => ({ ...bef, utm_campaign: v }))}
                    className="w-full"
                />
            </div>
            <p className="sm:col-span-3 text-[11px] text-slate-400 -mt-1">
                Leave a field empty to use the placeholder default. utm_content is set per link from its text
                (for example <span className="font-mono">pricing</span>), so each link is attributed on its own.
            </p>
        </div>
    );
}

// Mirrors the backend's slug: lowercase words joined with underscores.
function utmSlug(s: string): string {
    return s
        .toLowerCase()
        .trim()
        .split(/[^\p{L}\p{N}]+/u)
        .filter(Boolean)
        .join("_")
        .slice(0, 64);
}
