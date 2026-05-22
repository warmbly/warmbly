import HeadSelectMenu from "@/components/app/head/HeadSelectMenu";
import SelectOption from "@/components/app/popup/select/SelectOption";
import Search from "@/components/app/Search";
import { useUserProfile } from "@/hooks/context/user";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import type Folder from "@/lib/api/models/app/Folder";
import { RiFolderLine, RiSoundModuleLine } from "@remixicon/react";
import React, { useMemo } from "react";
import { Link } from "react-router-dom";
import {
    CalendarIcon,
    ChevronRightIcon,
    FilterIcon,
    MegaphoneIcon,
    PlusIcon,
} from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

const DefaultFolder = {
    title: "All folders",
    color: "#c4c8cf",
} as Folder;

export default function CampaignsPage() {
    const [folder, setFolder] = React.useState<string>("");
    const [query, setQuery] = React.useState<string>("");
    const campaignsData = useCampaigns({ query, folder });
    const p = useUserProfile();

    const sfolder = useMemo(() => {
        if (!p) return DefaultFolder;
        const f = p.user.folders.find((f) => f.id === folder);
        if (!f) return DefaultFolder;
        return f;
    }, [folder, p]);

    return (
        <Page width="wide">
            <PageHeader
                title="Campaigns"
                subtitle="Outreach sequences. Click a card to edit and monitor."
            >
                <button className="flex items-center gap-1.5 bg-sky-600 hover:bg-sky-700 text-white text-[13px] font-medium rounded-lg px-3 py-1.5 transition-colors">
                    <PlusIcon className="w-3.5 h-3.5" />
                    <span>New campaign</span>
                </button>
            </PageHeader>

            <div className="rounded-xl border border-slate-200/80 bg-white overflow-hidden">
                <div className="flex items-center justify-between px-4 py-3 border-b border-slate-100">
                    <div className="flex items-center gap-2">
                        <Search value={query} onChange={(v) => setQuery(v)} />
                        <HeadSelectMenu icon={<FilterIcon className="w-3.5 h-3.5" />} title={sfolder.title}>
                            {p?.user.folders.map((fo) => (
                                <SelectOption
                                    key={fo.id}
                                    onClick={async () => (folder !== fo.id ? setFolder(fo.id) : setFolder(""))}
                                    color={fo.color}
                                    selected={folder === fo.id}
                                >
                                    <RiFolderLine className="w-3.5 h-3.5" />
                                    <span className="truncate">{fo.title}</span>
                                </SelectOption>
                            ))}
                            <SelectOption onClick={() => p?.setFoldersEdit(true)}>
                                <RiSoundModuleLine className="w-3.5 h-3.5" />
                                <span className="truncate">Manage folders</span>
                            </SelectOption>
                        </HeadSelectMenu>
                    </div>
                </div>

                {campaignsData.isLoading ? (
                    <div className="p-4 space-y-2">
                        {[...Array(4)].map((_, i) => (
                            <div key={i} className="h-16 bg-slate-100 animate-pulse rounded-lg" />
                        ))}
                    </div>
                ) : campaignsData.isError ? (
                    <div className="p-6">
                        <EmptyState
                            icon={<MegaphoneIcon className="w-5 h-5" />}
                            title="Couldn't load campaigns"
                            description={
                                campaignsData.error?.message ||
                                "The request failed. Check that the backend is up and you're signed in to an organisation."
                            }
                        >
                            <button
                                onClick={() => campaignsData.refetch()}
                                className="bg-slate-900 hover:bg-slate-800 text-white text-[13px] font-medium rounded-lg px-3 py-1.5 transition-colors"
                            >
                                Retry
                            </button>
                        </EmptyState>
                    </div>
                ) : campaignsData.campaigns.length === 0 ? (
                    <div className="p-6">
                        <EmptyState
                            icon={<MegaphoneIcon className="w-5 h-5" />}
                            title="No campaigns yet"
                            description="Create your first campaign to start reaching prospects."
                        >
                            <button className="flex items-center gap-1.5 bg-sky-600 hover:bg-sky-700 text-white text-[13px] font-medium rounded-lg px-3 py-1.5 transition-colors">
                                <PlusIcon className="w-3.5 h-3.5" />
                                New campaign
                            </button>
                        </EmptyState>
                    </div>
                ) : (
                    <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-3 p-4">
                        {campaignsData.campaigns.map((c) => (
                            <Link
                                to={`/app/campaigns/${c.id}`}
                                key={c.id}
                                className="rounded-xl border border-slate-200/80 p-4 bg-white hover:border-sky-200 hover:shadow-sm transition-all group"
                            >
                                <div className="flex items-start justify-between mb-3">
                                    <div className="w-9 h-9 rounded-lg bg-sky-50 flex items-center justify-center shrink-0">
                                        <MegaphoneIcon className="w-4 h-4 text-sky-600" />
                                    </div>
                                    <ChevronRightIcon className="w-4 h-4 text-slate-300 opacity-0 group-hover:opacity-100 transition-opacity mt-1" />
                                </div>
                                <h3 className="text-sm font-medium text-slate-900 truncate mb-1">{c.name}</h3>
                                {c.description && (
                                    <p className="text-xs text-slate-400 truncate mb-3">{c.description}</p>
                                )}
                                <div className="flex items-center gap-3 text-[11px] text-slate-400">
                                    <span className="flex items-center gap-1">
                                        <CalendarIcon className="w-3 h-3" />
                                        {c.created_at
                                            ? new Date(c.created_at).toLocaleDateString("en-US", {
                                                  month: "short",
                                                  day: "numeric",
                                              })
                                            : "--"}
                                    </span>
                                    <span
                                        className={`flex items-center gap-1 ${
                                            c.status === "active"
                                                ? "text-emerald-600"
                                                : c.status === "draft"
                                                  ? "text-slate-400"
                                                  : "text-amber-600"
                                        }`}
                                    >
                                        <span
                                            className={`w-1.5 h-1.5 rounded-full ${
                                                c.status === "active"
                                                    ? "bg-emerald-500"
                                                    : c.status === "draft"
                                                      ? "bg-slate-300"
                                                      : "bg-amber-500"
                                            }`}
                                        />
                                        {c.status || "draft"}
                                    </span>
                                </div>
                            </Link>
                        ))}
                    </div>
                )}
            </div>
        </Page>
    );
}
