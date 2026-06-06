// Contact ordering settings — how the campaign picks who to send to next.
// On-theme: reuses the shared OptionSelect / Segmented / SettingRow primitives
// so it matches the rest of the settings page.

import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import { Label, TextInput } from "@/components/ui/field";
import { OptionSelect, Segmented, SettingRow } from "./components/CampaignPreferenceBoolBox";

const ORDER_OPTIONS: { value: Campaign["contact_order_by"]; label: string; hint: string }[] = [
    { value: "created_at", label: "Creation time", hint: "When the contact was added" },
    { value: "email", label: "Email", hint: "Alphabetical by email address" },
    { value: "name", label: "Name", hint: "Alphabetical by first, then last name" },
    { value: "custom_field", label: "Custom field", hint: "Order by a custom contact field" },
];

interface CampaignContactOrderProps {
    campaign: Campaign;
    newCampaign: Campaign;
    setNewCampaign: React.Dispatch<React.SetStateAction<Campaign>>;
}

export default function CampaignContactOrder({ newCampaign, setNewCampaign }: CampaignContactOrderProps) {
    return (
        <div className="space-y-5">
            {/* Order by — same card picker as the rest of settings */}
            <div>
                <Label>Order contacts by</Label>
                <OptionSelect
                    aria-label="Order contacts by"
                    cols={2}
                    value={newCampaign.contact_order_by}
                    onChange={(v) => setNewCampaign((prev) => ({ ...prev, contact_order_by: v }))}
                    options={ORDER_OPTIONS}
                />
            </div>

            {/* Direction */}
            <SettingRow
                title="Direction"
                description={
                    newCampaign.contact_order_dir === "desc"
                        ? "Z → A / newest → oldest"
                        : "A → Z / oldest → newest"
                }
                control={
                    <Segmented
                        value={newCampaign.contact_order_dir}
                        onChange={(v) => setNewCampaign((prev) => ({ ...prev, contact_order_dir: v }))}
                        options={[
                            { value: "asc", label: "Ascending" },
                            { value: "desc", label: "Descending" },
                        ]}
                    />
                }
            />

            {/* Custom field name */}
            {newCampaign.contact_order_by === "custom_field" && (
                <div>
                    <Label>Custom field name</Label>
                    <TextInput
                        value={newCampaign.contact_order_field || ""}
                        placeholder="e.g. company_size, priority"
                        onChange={(v) => setNewCampaign((prev) => ({ ...prev, contact_order_field: v }))}
                        className="w-full max-w-[280px]"
                    />
                    <p className="text-[11px] text-slate-400 mt-1.5">
                        Enter the name of a custom field from your contacts.
                    </p>
                </div>
            )}
        </div>
    );
}
