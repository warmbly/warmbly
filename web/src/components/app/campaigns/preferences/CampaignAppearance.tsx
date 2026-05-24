import type Campaign from "@/lib/api/models/app/campaigns/Campaign"
import SubTitle from "../../text/SubTitle"
import Title from "../../text/Title"
import MiniInput from "../../popup/MiniInput"
import MiniTextArea from "../../popup/MiniTextArea"
import CampaignPreferenceBoolBox from "./components/CampaignPreferenceBoolBox"
import Switch from "../../Switch"

export default function CampaignAppearance({
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
                <SubTitle>Campaign Name</SubTitle>
                <MiniInput
                    value={newCampaign.name}
                    placeholder={campaign.name}
                    onChange={(e) => setNewCampaign(bef => ({
                        ...bef,
                        name: e.target.value,
                    }))}
                />
            </div>
            <div>
                <SubTitle>Campaign Description</SubTitle>
                <MiniTextArea
                    value={newCampaign.description}
                    placeholder={campaign.description}
                    onChange={(e) => setNewCampaign(bef => ({
                        ...bef,
                        description: e.target.value,
                    }))}
                />
            </div>
            <CampaignPreferenceBoolBox>
                <div>
                    <Title>Plain Text Only</Title>
                    <SubTitle>Send as simple text email for the best deliverability (disables tracking)</SubTitle>
                </div>
                <Switch
                    id="campaign-pref-text"
                    value={newCampaign.text_only}
                    onChange={(e) => setNewCampaign(bef => ({
                        ...bef,
                        text_only: e,
                    }))}
                />
            </CampaignPreferenceBoolBox>
            <CampaignPreferenceBoolBox>
                <div className="min-w-0 flex-1">
                    <Title>Open Tracking</Title>
                    <SubTitle>Track email opens, but may slightly reduce deliverability</SubTitle>
                </div>
                <Switch
                    id="campaign-pref-open-tracking"
                    value={newCampaign.open_tracking}
                    onChange={(e) => setNewCampaign(bef => ({
                        ...bef,
                        open_tracking: e,
                    }))}
                />
            </CampaignPreferenceBoolBox>
            <CampaignPreferenceBoolBox>
                <div>
                    <Title>Link Tracking</Title>
                    <SubTitle>Track clicks on links to measure engagement (click-through rate)</SubTitle>
                </div>
                <Switch
                    id="campaign-pref-link-tracking"
                    value={newCampaign.link_tracking}
                    onChange={(e) => setNewCampaign(bef => ({
                        ...bef,
                        link_tracking: e,
                    }))}
                />
            </CampaignPreferenceBoolBox>
        </div>
    )
}
