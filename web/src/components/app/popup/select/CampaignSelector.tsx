import React from "react";
import Selector from "./Selector";
import SelectMenu from "./SelectMenu";
import SelectOption from "./SelectOption";
import { RiSearchLine, RiSendPlaneLine } from "@remixicon/react";
import useClickOutside from "@/hooks/useClickOutside";
import MiniInput from "../MiniInput";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";

export default function CampaignSelector({ onAdd, onRemove, selected, reverse }: { onAdd: (id: string, name?: string) => void, onRemove: (id: string) => void, selected: MiniCampaign[], reverse?: boolean }) {
    const [enabled, setEnabled] = React.useState<boolean>(false);
    const [query, setQuery] = React.useState<string>("");
    const c = useCampaigns({
        query,
        folder: "",
        enabled,
    });
    const [search, setSearch] = React.useState<string>("");

    const [show, setShow] = React.useState<boolean>(false);

    React.useEffect(() => {
        if (!show) return;
        setEnabled(true)
    }, [show])

    const popupRef = React.useRef<HTMLDivElement>(null);

    useClickOutside(popupRef, () => setShow(false))

    return (
        <div className="relative" ref={popupRef}>
            <Selector show={show} setShow={setShow} caret>
                <div className="flex gap-1 flex-wrap select-none">
                    {selected.length > 0 ? selected.map((v) => {
                        return (
                            <div key={v.id} className="flex gap-2 cursor-default items-center py-px px-3 rounded-full bg-slate-100">
                                <div
                                >
                                    <RiSendPlaneLine className="w-3 shrink-0" />
                                </div>
                                <span className="text-sm">{v.name}</span>
                            </div>
                        )
                    }) : (
                        <span className="text-slate-400 py-px">No campaigns selected...</span>
                    )}
                </div>
            </Selector>
            <SelectMenu show={show} reverse={reverse}>
                <form onSubmit={(e) => {
                    e.preventDefault();
                    setQuery(search)
                }}>
                    <div className="flex gap-2 mb-2">
                        <MiniInput
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            placeholder="Search..."
                        />
                        <button
                            type="submit"
                            className="w-12 flex cursor-pointer items-center justify-center text-white rounded-lg border border-transparent bg-blue-500 hover:bg-blue-600 transition shrink-0"
                        >
                            <RiSearchLine className="w-3 shrink-0" />
                        </button>
                    </div>
                </form>
                {c && c.campaigns !== null ? <>
                    {c.campaigns.length > 0 ? c.campaigns.map((camp, ind) => {
                        const isSelected = selected.some((v) => v.id === camp.id)
                        return (
                            <SelectOption
                                key={ind}
                                onClick={() => {
                                    if (isSelected) {
                                        onRemove(camp.id)
                                    } else {
                                        onAdd(camp.id, camp.name)
                                    }
                                }}
                                selected={isSelected}>
                                <RiSendPlaneLine className="w-4 shrink-0" />
                                {camp.name}
                            </SelectOption>
                        )
                    })
                        : <>
                            <p className="text-slate-400 text-center py-3">No result found.</p>
                        </>}
                </> : <>
                    <div className="animate-pulse space-y-1">
                        <div className="bg-slate-200 h-10 rounded-lg" />
                        <div className="bg-slate-200 h-10 rounded-lg" />
                        <div className="bg-slate-200 h-10 rounded-lg" />
                        <div className="bg-slate-200 h-10 rounded-lg" />
                    </div>
                </>}
            </SelectMenu>
        </div>
    )
}
