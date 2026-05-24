import ContactsTable from "@/components/app/contacts/ContactsTable";
import { useCampaign } from "@/hooks/context/campaign";

export default function CampaignLeads() {
    const campaign = useCampaign();
    if (!campaign) {
        throw new Error("CampaignContacts cannot be rendered without a campaign")
    }

    return (
        <ContactsTable
            current_campaign={{
                name: campaign.name,
                id: campaign.id,
            }}
        />
    )
}
