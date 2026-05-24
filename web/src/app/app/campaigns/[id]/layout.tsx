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

    return (
        <>
            {!campaignData.isLoading ? (
                <div className="p-5 space-y-3">
                    {[...Array(5)].map((_, i) => (
                        <div key={i} className="h-10 bg-zinc-100 animate-pulse rounded-lg" />
                    ))}
                </div>
            ) : (
                <CampaignContext.Provider value={campaignData.data}>
                    <div>
                        <div className="px-5 pt-5 pb-0">
                            <h1 className="text-xl font-semibold text-zinc-900 mb-0.5">{campaignData.data.name}</h1>
                            <p className="text-[13px] text-zinc-400 font-mono">{campaignData.data.id}</p>
                        </div>
                        <div className="flex items-center gap-0.5 px-5 pt-3 pb-0 border-b border-zinc-200 overflow-x-auto no-scrollbar">
                            {Object.entries(tabData).map(([label, path]) => {
                                const fullPath = `/app/campaigns/${id}${path}`;
                                const isActive = pathname.replaceAll("/", "") === fullPath.replaceAll("/", "");
                                return (
                                    <Link
                                        key={path}
                                        to={fullPath}
                                        className={`relative px-3 py-2 text-[13px] font-medium transition-colors duration-100 ${isActive
                                            ? "text-zinc-900"
                                            : "text-zinc-400 hover:text-zinc-900"
                                            }`}
                                    >
                                        {label}
                                        {isActive && (
                                            <span className="absolute bottom-0 left-3 right-3 h-0.5 border-b-2 border-zinc-900" />
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
            )}
        </>
    )
}
