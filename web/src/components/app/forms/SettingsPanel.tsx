// SettingsPanel — what happens after a submission, plus spam protection.
//
// Laid out like the campaign preferences page: stacked sections split by
// hairlines, each a heading over a responsive grid of controls. Controls
// spread across the width rather than sitting in one narrow column, and each
// declares how much room it needs with a col-span.

import React from "react";
import { PlusIcon, XIcon } from "lucide-react";

import { Label, TextInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { SettingRow, Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";

import Section from "./SettingsSection";

/** The API's code for a redirect destination outside the workspace's own domains. */
export const REDIRECT_NOT_OWN_DOMAIN = "redirect_target_not_own_domain";

export interface FormSettingsDraft {
    success_message: string;
    redirect_url: string;
    campaign_id: string | null;
    category_ids: string[];
    allowed_domains: string[];
    captcha_enabled: boolean;
    triage_enabled: boolean;
}

export default function SettingsPanel({
    draft,
    redirectError = "",
    redirectInactive = false,
    captchaAvailable,
    triageAvailable,
    onChange,
}: {
    draft: FormSettingsDraft;
    /** The API's refusal of the redirect destination, shown under the field. */
    redirectError?: string;
    /** The saved destination is not on the workspace's own domains, so visitors see the success message. */
    redirectInactive?: boolean;
    captchaAvailable: boolean;
    triageAvailable: boolean;
    onChange: (patch: Partial<FormSettingsDraft>) => void;
}) {
    const campaigns = useCampaigns({ query: "", folder: "" });
    const [domainInput, setDomainInput] = React.useState("");

    function addDomain() {
        const host = domainInput
            .trim()
            .toLowerCase()
            .replace(/^https?:\/\//, "")
            .replace(/[/?#].*$/, "");
        if (!host) return;
        if (!draft.allowed_domains.includes(host)) {
            onChange({ allowed_domains: [...draft.allowed_domains, host] });
        }
        setDomainInput("");
    }

    return (
        <div className="px-4 lg:px-6 py-5 divide-y divide-slate-200/60">
            <Section
                title="After submission"
                description="What the visitor sees the moment they finish the form."
            >
                <div className="xl:col-span-2">
                    <Label>Success message</Label>
                    <textarea
                        value={draft.success_message}
                        onChange={(e) => onChange({ success_message: e.target.value })}
                        rows={3}
                        className="w-full rounded-md border border-slate-200 px-2.5 py-1.5 text-[16px] md:text-[12.5px] text-slate-900 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                    />
                </div>
                <div>
                    <Label>Redirect URL</Label>
                    <TextInput
                        value={draft.redirect_url}
                        onChange={(v) => onChange({ redirect_url: v })}
                        placeholder="https://yourcompany.com/thanks (optional)"
                        invalid={redirectError !== ""}
                    />
                    {redirectError ? (
                        <p className="text-[11px] text-rose-600 mt-1" role="alert">
                            {redirectError}
                        </p>
                    ) : redirectInactive ? (
                        <p className="text-[11px] text-amber-700 mt-1">
                            Not in use: this page is no longer on one of your workspace's own domains, so visitors see
                            the success message instead. This happens when the mailbox or verified domain it relied on
                            was removed. Point it at one of your own domains, or reconnect that domain.
                        </p>
                    ) : null}
                    <p className="text-[11px] text-slate-500 mt-1">
                        Must be a page on one of your workspace's own domains: a domain you have a mailbox on (not
                        gmail.com or another shared provider), your verified tracking domain, your verified forms
                        domain, a website confirmed under Sending domains, or a subdomain of one. Leave empty to show
                        the success message instead.
                    </p>
                </div>
            </Section>

            <Section
                title="Lead capture"
                description="Where a submitted contact lands in your workspace."
            >
                <div>
                    <Label>Add labels</Label>
                    <CategoryPicker
                        value={draft.category_ids}
                        onChange={(next) => onChange({ category_ids: next })}
                        placeholder="Pick labels, e.g. Website leads"
                    />
                    <p className="text-[11px] text-slate-500 mt-1">Every submitted contact is filed under these.</p>
                </div>
                <div>
                    <Label>Add to campaign</Label>
                    <SelectMenu
                        value={draft.campaign_id ?? ""}
                        onChange={(v) => onChange({ campaign_id: v === "" ? null : v })}
                        options={[
                            { value: "", label: "None" },
                            ...(campaigns.campaigns ?? []).map((c) => ({ value: c.id, label: c.name })),
                        ]}
                        fullWidth
                        aria-label="Campaign"
                    />
                    <p className="text-[11px] text-slate-500 mt-1">
                        New contacts join this campaign as leads. Sending still follows the campaign's own schedule and
                        limits. The campaign keeps running for new leads instead of finishing between submissions.
                    </p>
                </div>
            </Section>

            <Section
                title="Spam protection"
                description="Every form already has a honeypot trap, a minimum fill time and per-address rate limits."
            >
                {captchaAvailable ? (
                    <div className="col-span-full">
                            <SettingRow
                            title="Captcha challenge"
                            description="Ask visitors to pass a Cloudflare Turnstile check before submitting."
                        >
                            <Toggle
                                value={draft.captcha_enabled}
                                onChange={(v) => onChange({ captcha_enabled: v })}
                            />
                        </SettingRow>
                    </div>
                ) : (
                    <p className="col-span-full text-[11.5px] text-slate-500 rounded-md bg-slate-50 border border-slate-200 px-3 py-2 max-w-3xl">
                        A captcha challenge becomes available once the operator configures Cloudflare Turnstile
                        (TURNSTILE_SECRET and TURNSTILE_SITE_KEY).
                    </p>
                )}
                {triageAvailable ? (
                    <div className="col-span-full">
                        <SettingRow
                            title="Triage submissions"
                            description="Classify each submission as a buyer, vendor, job seeker or junk. Junk stays in the list but creates no contact and joins no campaign. The submitted answers are sent to TypeSafe."
                        >
                            <Toggle value={draft.triage_enabled} onChange={(v) => onChange({ triage_enabled: v })} />
                        </SettingRow>
                    </div>
                ) : (
                    <p className="col-span-full text-[11.5px] text-slate-500 rounded-md bg-slate-50 border border-slate-200 px-3 py-2 max-w-3xl">
                        Submission triage becomes available once the operator configures TypeSafe (TYPESAFE_API_KEY).
                    </p>
                )}
            </Section>

            <Section
                title="Embedding"
                description="Which sites may host this form. Leave empty to allow any site."
            >
                <div className="xl:col-span-2">
                    <Label>Allowed domains</Label>
                    <div className="flex items-center gap-1.5 max-w-md">
                        <TextInput
                            value={domainInput}
                            onChange={setDomainInput}
                            placeholder="example.com"
                            className="flex-1"
                            onKeyDown={(e) => {
                                if (e.key === "Enter") {
                                    e.preventDefault();
                                    addDomain();
                                }
                            }}
                        />
                        <button
                            type="button"
                            onClick={addDomain}
                            aria-label="Add domain"
                            className="h-7 px-2 inline-flex items-center gap-1 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50 shrink-0"
                        >
                            <PlusIcon className="w-3 h-3" /> Add
                        </button>
                    </div>
                    {draft.allowed_domains.length > 0 && (
                        <div className="flex flex-wrap gap-1 mt-2">
                            {draft.allowed_domains.map((d) => (
                                <span
                                    key={d}
                                    className="inline-flex items-center gap-1 h-5 pl-2 pr-1 rounded bg-sky-50 text-sky-700 text-[11px]"
                                >
                                    {d}
                                    <button
                                        type="button"
                                        aria-label={`Remove ${d}`}
                                        onClick={() =>
                                            onChange({
                                                allowed_domains: draft.allowed_domains.filter((x) => x !== d),
                                            })
                                        }
                                        className="size-4 inline-flex items-center justify-center rounded hover:bg-sky-100"
                                    >
                                        <XIcon className="w-2.5 h-2.5" />
                                    </button>
                                </span>
                            ))}
                        </div>
                    )}
                    <p className="text-[11px] text-slate-500 mt-1.5">
                        With domains listed, only those sites and their subdomains can embed the form.
                    </p>
                </div>
            </Section>
        </div>
    );
}
