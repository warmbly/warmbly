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
    FilterIcon,
    MegaphoneIcon,
    PlusIcon,
} from "lucide-react";
import {
    EmptyBlock,
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    TopbarAction,
} from "@/components/layout/Page";

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

    const campaigns = campaignsData.campaigns ?? [];

    return (
        <Page>
            <PageTopbar
                eyebrow="Campaigns"
                subtitle={
                    campaignsData.isPending
                        ? "Loading…"
                        : campaignsData.isError
                            ? "Failed to load"
                            : `${campaigns.length} ${campaigns.length === 1 ? "sequence" : "sequences"}`
                }
            >
                <TopbarAction
                    onClick={() => {
                        /* TODO: open new-campaign modal */
                    }}
                    icon={<PlusIcon className="w-3 h-3" />}
                >
                    New campaign
                </TopbarAction>
            </PageTopbar>

            <SectionBar label="All" count={campaigns.length}>
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
            </SectionBar>

            <PageBody>
                {campaignsData.isPending ? (
                    <div className="divide-y divide-slate-200/60">
                        {Array.from({ length: 6 }).map((_, i) => (
                            <div key={i} className="h-11 px-5 flex items-center gap-3">
                                <div className="w-1.5 h-1.5 rounded-full bg-slate-200" />
                                <div className="h-3 w-44 bg-slate-100 rounded animate-pulse" />
                                <div className="ml-auto h-3 w-12 bg-slate-100 rounded animate-pulse" />
                            </div>
                        ))}
                    </div>
                ) : campaignsData.isError ? (
                    <EmptyBlock
                        title="Couldn't load campaigns"
                        body={
                            campaignsData.error?.message ||
                            "The request failed. Check that the backend is up and you're signed in."
                        }
                        cta={
                            <TopbarAction onClick={() => campaignsData.refetch()} variant="ghost">
                                Retry
                            </TopbarAction>
                        }
                    />
                ) : campaigns.length === 0 ? (
                    <EmptyBlock
                        title="No campaigns yet"
                        body="Create your first sequence to start reaching prospects."
                        cta={
                            <TopbarAction icon={<PlusIcon className="w-3 h-3" />}>
                                New campaign
                            </TopbarAction>
                        }
                    />
                ) : (
                    <div className="divide-y divide-slate-200/60">
                        {campaigns.map((c) => {
                            const status = c.status || "draft";
                            const dot =
                                status === "active"
                                    ? "bg-emerald-500"
                                    : status === "draft"
                                        ? "bg-slate-300"
                                        : "bg-amber-500";
                            return (
                                <Link
                                    key={c.id}
                                    to={`/app/campaigns/${c.id}`}
                                    className="group h-11 px-5 flex items-center gap-3 hover:bg-slate-50 transition-colors"
                                >
                                    <span className={`size-1.5 rounded-full shrink-0 ${dot}`} />
                                    <MegaphoneIcon className="w-3.5 h-3.5 text-slate-400 shrink-0" />
                                    <span className="text-[12.5px] text-slate-900 font-medium truncate">
                                        {c.name}
                                    </span>
                                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums">
                                        {c.id.slice(0, 8)}
                                    </span>
                                    {c.description && (
                                        <span className="text-[11.5px] text-slate-400 truncate hidden md:inline">
                                            {c.description}
                                        </span>
                                    )}
                                    <span className="ml-auto text-[10px] uppercase tracking-[0.1em] text-slate-400 font-medium">
                                        {status}
                                    </span>
                                    <span className="font-mono text-[10.5px] text-slate-400 tabular-nums flex items-center gap-1">
                                        <CalendarIcon className="w-3 h-3" />
                                        {c.created_at
                                            ? new Date(c.created_at).toLocaleDateString("en-US", {
                                                month: "short",
                                                day: "numeric",
                                            })
                                            : "--"}
                                    </span>
                                </Link>
                            );
                        })}
                    </div>
                )}
            </PageBody>
        </Page>
    );
}
