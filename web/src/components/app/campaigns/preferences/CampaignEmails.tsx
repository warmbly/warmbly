import type Campaign from "@/lib/api/models/app/campaigns/Campaign"
import SubTitle from "../../text/SubTitle"
import TagSelector from "../../popup/select/TagSelector"
import MiniNumberInput from "../../popup/MiniNumberInput"
import CampaignPreferenceBoolBox from "./components/CampaignPreferenceBoolBox"
import Title from "../../text/Title"
import Switch from "../../Switch"
import MultiInput from "../../MultiInput"

export default function CampaignEmails({
    campaign,
    newCampaign,
    setNewCampaign,
}: {
    campaign: Campaign,
    newCampaign: Campaign,
    setNewCampaign: React.Dispatch<React.SetStateAction<Campaign>>,
}) {
    return (
        <div className="space-y-6">
            <div>
                <SubTitle>Email Accounts</SubTitle>
                <TagSelector
                    selected={newCampaign.email_tags}
                    onAdd={(t) => setNewCampaign(bef => ({
                        ...bef,
                        email_tags: [...bef.email_tags, t]
                    }))}
                    onRemove={(t) => setNewCampaign(bef => ({
                        ...bef,
                        email_tags: bef.email_tags.filter((e) => e !== t)
                    }))}
                />
            </div>
            <div>
                <SubTitle>Daily Limit</SubTitle>
                <MiniNumberInput
                    value={newCampaign.daily_limit}
                    placeholder={`${campaign.daily_limit}`}
                    onChange={(e) => setNewCampaign(bef => ({
                        ...bef,
                        daily_limit: e.target.valueAsNumber
                    }))}
                />
            </div>
            <CampaignPreferenceBoolBox>
                <div>
                    <Title>Unsubscribe Header</Title>
                    <SubTitle>Add an unsubscribe link in the email header for compliance and better deliverability</SubTitle>
                </div>
                <Switch
                    id="campaign-pref-unsub"
                    value={newCampaign.unsubscribe_header}
                    onChange={(e) => setNewCampaign(bef => ({
                        ...bef,
                        unsubscribe_header: e,
                    }))}
                />
            </CampaignPreferenceBoolBox>
            <CampaignPreferenceBoolBox>
                <div>
                    <Title>Risky Emails</Title>
                    <SubTitle>Attempt sending to risky addresses (may increase bounces)</SubTitle>
                </div>
                <Switch
                    id="campaign-pref-risk"
                    value={newCampaign.risky_emails}
                    onChange={(e) => setNewCampaign(bef => ({
                        ...bef,
                        risky_emails: e,
                    }))}
                />
            </CampaignPreferenceBoolBox>
            <hr className="text-slate-200 my-5" />
            <div className="space-y-10">
                <div>
                    <Title>CC receipments</Title>
                    <SubTitle>Send a copy of each email to additional recipients (visible to others)</SubTitle>
                    <MultiInput
                        values={newCampaign.cc}
                        onChange={(v) => setNewCampaign(bef => ({
                            ...bef,
                            cc: v,
                        }))}
                    />
                </div>
                <div>
                    <Title>BCC receipments</Title>
                    <SubTitle>Send a blind copy of each email to hidden recipients (not visible to others)</SubTitle>
                    <MultiInput
                        values={newCampaign.bcc}
                        onChange={(v) => setNewCampaign(bef => ({
                            ...bef,
                            bcc: v,
                        }))}
                    />
                </div>
            </div>
        </div>
    )
}
