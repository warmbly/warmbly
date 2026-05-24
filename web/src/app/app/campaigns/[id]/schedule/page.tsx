import React from "react";
import DateSelect from "@/components/app/campaigns/schedule/ScheduleDateSelect";
import WeekdayBitmask from "@/components/app/campaigns/schedule/WeekdayBitmask";
import { Loading } from "@/components/loader";
import { RiHistoryLine } from "@remixicon/react";
import Selector from "@/components/app/popup/select/Selector";
import SelectMenu from "@/components/app/popup/select/SelectMenu";
import { twColors } from "tailwindv4-colors";
import SelectOption from "@/components/app/popup/select/SelectOption";
import TimeSelector from "@/components/app/popup/select/TimeSelector";
import SubTitle from "@/components/app/text/SubTitle";
import { useCampaign } from "@/hooks/context/campaign";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { useUserProfile } from "@/hooks/context/user";

export default function CampaignSchedule() {
    const campaign = useCampaign();
    if (!campaign) {
        throw new Error("CampaignSchedule cannot be rendered without a campaign")
    }

    const updateCampaign = useUpdateCampaign(campaign.id);

    const [loading, setLoading] = React.useState<boolean>(false);
    const [newData, setNewData] = React.useState<Campaign | null>(campaign)

    React.useEffect(() => {
        if (!campaign) return;
        setNewData(campaign)
    }, [campaign])


    const getChanges = () => {
        if (!newData || !campaign) return {};
        return {
            ...(newData.start_date !== campaign.start_date && { start_date: newData.start_date }),
            ...(newData.end_date !== campaign.end_date && { end_date: newData.end_date }),
            ...(newData.timezone !== campaign.timezone && { timezone: newData.timezone }),
            ...(newData.days !== campaign.days && { days: newData.days }),
            ...(newData.start_time !== campaign.start_time && { start_time: newData.start_time }),
            ...(newData.end_time !== campaign.end_time && { end_time: newData.end_time })
        }
    }

    async function submit() {
        if (loading || !campaign) return;
        try {
            setLoading(true);
            const data = getChanges();
            await toast.promise(
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

    const u = useUserProfile();
    const tzRef = React.useRef<HTMLDivElement>(null);
    const [tzDrop, setTzDrop] = React.useState<boolean>(false);

    React.useEffect(() => {
        const handleClickOutside = (event: MouseEvent) => {
            if (tzRef.current && !tzRef.current.contains(event.target as Node)) {
                setTzDrop(false);
            }
        };
        if (tzDrop) {
            document.addEventListener('mousedown', handleClickOutside);
        }

        return () => {
            document.removeEventListener('mousedown', handleClickOutside);
        };
    }, [tzDrop]);

    if (!campaign || !newData) return null;

    const hasChanges = Object.keys(getChanges() || {}).length > 0;

    return (<>
        <div className="flex flex-col md:flex-row gap-4">
            <div className="md:w-60 shrink-0 space-y-3">
                <DateSelect
                    title="Start Date"
                    value={newData.start_date ?? null}
                    onChange={(v) => setNewData(bef => bef ? ({
                        ...bef,
                        startDate: v,
                    }) : null)}
                />
                <DateSelect
                    title="End Date"
                    value={newData.end_date ?? null}
                    onChange={(v) => setNewData(bef => bef ? ({
                        ...bef,
                        endDate: v,
                    }) : null)}
                />
            </div>
            <div className="flex-1 space-y-4">
                <div>
                    <SubTitle>Timezone</SubTitle>
                    <div ref={tzRef} className="relative">
                        <Selector
                            caret
                            show={tzDrop}
                            setShow={setTzDrop}
                        >{((() => {
                            const v = u.timezones.find((tz) => tz.name === newData.timezone)
                            if (v) {
                                return v.display_name
                            } else {
                                return newData.timezone
                            }
                        })())}</Selector>
                        <SelectMenu
                            show={tzDrop}
                        >
                            {u.timezones ? (<div className="space-y-1">
                                {u.timezones.map((tz) => (
                                    <SelectOption
                                        key={tz.name}
                                        onClick={() => {
                                            setNewData(bef => bef ? ({
                                                ...bef,
                                                timezone: tz.name,
                                            }) : null)
                                        }}
                                        selected={tz.name === newData.timezone}
                                    >
                                        <RiHistoryLine className="w-4 h-4" />
                                        <span>{tz.display_name}</span>
                                    </SelectOption>
                                ))}
                            </div>) : (
                                <div className="flex justify-center">
                                    <Loading className="h-8 w-8" color={twColors.slate[200]} />
                                </div>
                            )}
                        </SelectMenu>
                    </div>
                </div>
                <div className="grid md:grid-cols-2 gap-4">
                    <div>
                        <SubTitle>Active Days</SubTitle>
                        <div className="space-y-1">
                            <WeekdayBitmask
                                weekdays={["Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"]}
                                value={newData.days}
                                setValue={(v) => setNewData(bef => bef ? ({
                                    ...bef,
                                    days: v,
                                }) : null)}
                            />
                        </div>
                    </div>
                    <div className="space-y-4">
                        <div>
                            <SubTitle>Start Time</SubTitle>
                            <TimeSelector
                                value={newData.start_time}
                                onChange={(v) => setNewData(bef => bef ? ({
                                    ...bef,
                                    start_time: v,
                                }) : null)}
                            />
                        </div>
                        <div>
                            <SubTitle>End Time</SubTitle>
                            <TimeSelector
                                value={newData.end_time}
                                onChange={(v) => setNewData(bef => bef ? ({
                                    ...bef,
                                    end_time: v,
                                }) : null)}
                            />
                        </div>
                    </div>
                </div>
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
    </>);
}
