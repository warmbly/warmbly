import { Link, Outlet, useLocation, useParams } from "react-router-dom";
import useCampaign from "@/lib/api/hooks/app/campaigns/useCampaign";
import { CampaignContext } from "@/hooks/context/campaign";

export default function CampaignLayout() {
    const location = useLocation()
    const { id } = useParams()
    const { pathname } = location;
    const campaignData = useCampaign(id ?? "")

    const tabData = {
        "Analytics": "",
        "Leads": "/leads",
        "Sequences": "/sequences",
        "Schedule": "/schedule",
        "Preferences": "/preferences"
    }

    if (campaignData.isLoading) {
        return (
            <div className="p-5 space-y-3">
                {[...Array(5)].map((_, i) => (
                    <div key={i} className="h-10 bg-slate-100 animate-pulse rounded-lg" />
                ))}
            </div>
        );
    }

    if (campaignData.isError || !campaignData.data) {
        return (
            <div className="flex flex-col items-center justify-center py-24 text-center">
                <p className="text-[13px] font-medium text-slate-900">Couldn't load this campaign</p>
                <p className="text-[12px] text-slate-400 mt-1 max-w-[34ch]">
                    It may have been deleted, or you don't have access in this workspace.
                </p>
                <Link
                    to="/app/campaigns"
                    className="mt-4 inline-flex items-center h-8 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium transition-colors"
                >
                    Back to campaigns
                </Link>
            </div>
        );
    }

    return (
        <CampaignContext.Provider value={campaignData.data}>
            <div>
                <div className="px-5 pt-5 pb-0">
                    <h1 className="text-xl font-semibold text-slate-900 mb-0.5">{campaignData.data.name}</h1>
                    <p className="text-[13px] text-slate-400 font-mono">{campaignData.data.id}</p>
                </div>
                <div className="flex items-center gap-0.5 px-5 pt-3 pb-0 border-b border-slate-200 overflow-x-auto no-scrollbar">
                    {Object.entries(tabData).map(([label, path]) => {
                        const fullPath = `/app/campaigns/${id}${path}`;
                        const isActive = pathname.replaceAll("/", "") === fullPath.replaceAll("/", "");
                        return (
                            <Link
                                key={path}
                                to={fullPath}
                                className={`relative px-3 py-2 text-[13px] font-medium transition-colors duration-100 ${isActive
                                    ? "text-slate-900"
                                    : "text-slate-400 hover:text-slate-900"
                                    }`}
                            >
                                {label}
                                {isActive && (
                                    <span className="absolute bottom-0 left-3 right-3 h-0.5 border-b-2 border-slate-900" />
                                )}
                            </Link>
                        );
                    })}
                </div>
                <div className="p-5">
                    <Outlet />
                </div>
            </div>
        </CampaignContext.Provider>
    )
}
