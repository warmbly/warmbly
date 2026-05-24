import { useCampaign } from "@/hooks/context/campaign";
import TaskPreview from "@/components/app/campaigns/TaskPreview";
import { BarChart3Icon } from "lucide-react";

export default function CampaignPreview() {
    const campaign = useCampaign();

    if (!campaign) {
        return (
            <div className="space-y-3">
                {[...Array(3)].map((_, i) => (
                    <div key={i} className="h-16 bg-zinc-100 animate-pulse rounded-lg" />
                ))}
            </div>
        );
    }

    return (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
            <div className="lg:col-span-2">
                <div className="bg-white rounded-xl border border-zinc-200 p-5 flex flex-col items-center justify-center min-h-[280px]">
                    <div className="w-10 h-10 rounded-xl bg-zinc-100 flex items-center justify-center mb-3">
                        <BarChart3Icon className="w-4 h-4 text-zinc-400" />
                    </div>
                    <h2 className="text-sm font-medium text-zinc-900 mb-0.5">Campaign Analytics</h2>
                    <p className="text-xs text-zinc-400">Data will appear here once the campaign starts sending.</p>
                </div>
            </div>

            <div className="lg:col-span-1">
                <TaskPreview
                    campaignId={campaign.id}
                    campaignStatus={campaign.status}
                />
            </div>
        </div>
    );
}
