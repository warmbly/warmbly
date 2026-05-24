import React from "react";
import { Loading } from "@/components/loader";
import { useCampaign } from "@/hooks/context/campaign";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import CampaignAppearance from "@/components/app/campaigns/preferences/CampaignAppearance";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import CampaignEmails from "@/components/app/campaigns/preferences/CampaignEmails";
import CampaignContactOrder from "@/components/app/campaigns/preferences/CampaignContactOrder";

export default function CampaignPreferences() {
    const campaign = useCampaign()
    if (!campaign) {
        throw new Error("CampaignPreferences cannot be rendered without a campaign")
    }

    const updateCampaign = useUpdateCampaign(campaign.id);

    const [loading, setLoading] = React.useState<boolean>(false);
    const [activeTab, setActiveTab] = React.useState<string>("tab1");
    const [newData, setNewData] = React.useState<Campaign>(campaign);

    React.useEffect(() => {
        if (!campaign) return;
        setNewData(campaign);
    }, [campaign])

    const tabData = {
        ...(campaign && {
            tab1: {
                title: "Appearance",
                content: <CampaignAppearance campaign={campaign} newCampaign={newData} setNewCampaign={setNewData} />,
            },
            tab2: {
                title: "Campaign Emails",
                content: <CampaignEmails campaign={campaign} newCampaign={newData} setNewCampaign={setNewData} />,
            },
            tab3: {
                title: "Contact Order",
                content: <CampaignContactOrder campaign={campaign} newCampaign={newData} setNewCampaign={setNewData} />,
            },
        }),
    };

    const getChanges = () => {
        if (!newData) return {};
        return {
            ...(newData.name !== campaign.name && { name: newData.name }),
            ...(newData.description !== campaign.description && { description: newData.description }),

            ...(newData.text_only !== campaign.text_only && { text_only: newData.text_only }),
            ...(newData.open_tracking !== campaign.open_tracking && { open_tracking: newData.open_tracking }),
            ...(newData.link_tracking !== campaign.link_tracking && { link_tracking: newData.link_tracking }),

            ...(newData.email_tags !== campaign.email_tags && { email_tags: newData.email_tags }),
            ...(newData.daily_limit !== campaign.daily_limit && { daily_limit: newData.daily_limit }),

            ...(newData.unsubscribe_header !== campaign.unsubscribe_header && { unsubscribe_header: newData.unsubscribe_header }),
            ...(newData.risky_emails !== campaign.risky_emails && { risky_emails: newData.risky_emails }),

            ...(newData.cc !== campaign.cc && { cc: newData.cc }),
            ...(newData.bcc !== campaign.bcc && { cc: newData.bcc }),

            ...(newData.contact_order_by !== campaign.contact_order_by && { contact_order_by: newData.contact_order_by }),
            ...(newData.contact_order_dir !== campaign.contact_order_dir && { contact_order_dir: newData.contact_order_dir }),
            ...(newData.contact_order_field !== campaign.contact_order_field && { contact_order_field: newData.contact_order_field }),
        }
    }

    async function submit() {
        if (loading) return;
        try {
            setLoading(true)
            const data = getChanges();
            toast.promise(
                updateCampaign.mutateAsync(data),
                {
                    loading: "Saving...",
                    success: "Campaign successfully updated.",
                    error: (err: AppError) => buildError(err),
                }
            )
        } finally {
            setLoading(false);
        }
    }

    const hasChanges = Object.keys(getChanges()).length > 0;

    return (
        <div>
            <div className="flex flex-col md:flex-row gap-4">
                <div className="flex md:flex-col gap-0.5 pb-3 md:pb-0 border-b md:border-b-0 md:border-r border-zinc-200 md:w-48 md:shrink-0 md:pr-4">
                    {Object.keys(tabData).map((key) => (
                        <button
                            key={key}
                            onClick={() => setActiveTab(key)}
                            className={`h-8 px-3 md:w-full text-left text-[13px] font-medium rounded-lg select-none cursor-pointer transition-colors duration-100 ${activeTab === key ? "bg-zinc-100 text-zinc-900" : "text-zinc-400 hover:text-zinc-900 hover:bg-zinc-50"}`}
                        >
                            {tabData[key as keyof typeof tabData]?.title}
                        </button>
                    ))}
                </div>
                <div className="flex-1 min-w-0">
                    {tabData[activeTab as keyof typeof tabData]?.content}
                </div>
            </div>
            <div className={`flex justify-end gap-2 mt-4 transition-opacity duration-100 ${hasChanges ? "opacity-100" : "opacity-40 pointer-events-none"}`}>
                <button
                    className="text-[13px] font-medium text-zinc-600 hover:text-zinc-900 border border-zinc-200 rounded-lg px-3 py-1.5 transition-colors duration-100"
                    onClick={() => setNewData(campaign)}
                >
                    Reset
                </button>
                <button
                    className="bg-zinc-900 text-white hover:bg-zinc-800 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors duration-100 min-w-[100px] flex items-center justify-center"
                    onClick={submit}
                >
                    {loading ? <Loading className="h-4" /> : "Save Changes"}
                </button>
            </div>
        </div>
    )
}
