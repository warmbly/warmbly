import React from "react";
import { ListOrderedIcon, SettingsIcon, SlidersHorizontalIcon } from "lucide-react";
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
import useCampaignSenders from "@/lib/api/hooks/app/campaigns/useCampaignSenders";
import useReplaceCampaignSenders from "@/lib/api/hooks/app/campaigns/useReplaceCampaignSenders";

const DAILY_MIN = 3;
const DAILY_MAX = 100;

const TABS = [
    { key: "standard", title: "Standard", Icon: SettingsIcon },
    { key: "advanced", title: "Advanced", Icon: SlidersHorizontalIcon },
    { key: "order", title: "Contact order", Icon: ListOrderedIcon },
] as const;

type TabKey = (typeof TABS)[number]["key"];

export default function CampaignPreferences() {
    const campaign = useCampaign();
    if (!campaign) {
        throw new Error("CampaignPreferences cannot be rendered without a campaign");
    }

    const updateCampaign = useUpdateCampaign(campaign.id);
    const replaceSenders = useReplaceCampaignSenders(campaign.id);

    // The explicit sender pool is managed by the senders endpoint (not PATCH).
    // Only fetch it when the campaign is actually on the explicit strategy.
    const { data: senders } = useCampaignSenders(
        campaign.id,
        campaign.sender_strategy === "explicit",
    );

    const [loading, setLoading] = React.useState(false);
    const [activeTab, setActiveTab] = React.useState<TabKey>("standard");
    const [newData, setNewData] = React.useState<Campaign>(campaign);

    // Selected mailbox ids for the "Specific accounts" mode + the saved baseline
    // we diff against. Seeded from the campaign's current senders.
    const [explicitAccounts, setExplicitAccounts] = React.useState<string[]>([]);
    const [savedAccounts, setSavedAccounts] = React.useState<string[]>([]);

    React.useEffect(() => {
        if (!campaign) return;
        setNewData(campaign);
    }, [campaign]);

    React.useEffect(() => {
        if (!senders) return;
        const ids = senders.map((s) => s.email_account_id);
        setExplicitAccounts(ids);
        setSavedAccounts(ids);
    }, [senders]);

    const getChanges = (): Partial<Campaign> => {
        if (!newData) return {};
        return {
            ...(newData.name !== campaign.name && { name: newData.name }),
            ...(newData.description !== campaign.description && { description: newData.description }),

            // Standard — sending + deliverability
            ...(newData.email_tags !== campaign.email_tags && { email_tags: newData.email_tags }),
            ...(newData.daily_limit !== campaign.daily_limit && { daily_limit: newData.daily_limit }),
            ...(newData.stop_on_reply !== campaign.stop_on_reply && { stop_on_reply: newData.stop_on_reply }),
            ...(newData.text_only !== campaign.text_only && { text_only: newData.text_only }),
            ...(newData.open_tracking !== campaign.open_tracking && { open_tracking: newData.open_tracking }),
            ...(newData.link_tracking !== campaign.link_tracking && { link_tracking: newData.link_tracking }),
            ...(newData.unsubscribe_header !== campaign.unsubscribe_header && {
                unsubscribe_header: newData.unsubscribe_header,
            }),

            // Advanced — sender selection + rotation
            ...(newData.sender_strategy !== campaign.sender_strategy && {
                sender_strategy: newData.sender_strategy,
            }),
            ...(newData.rotation_mode !== campaign.rotation_mode && { rotation_mode: newData.rotation_mode }),

            // Advanced — ramp-up
            ...(newData.ramp_enabled !== campaign.ramp_enabled && { ramp_enabled: newData.ramp_enabled }),
            ...(newData.ramp_start !== campaign.ramp_start && { ramp_start: newData.ramp_start }),
            ...(newData.ramp_increment !== campaign.ramp_increment && { ramp_increment: newData.ramp_increment }),
            ...(newData.ramp_ceiling !== campaign.ramp_ceiling && { ramp_ceiling: newData.ramp_ceiling }),

            // Advanced — ESP matching + new-lead throttle
            ...(newData.esp_match_mode !== campaign.esp_match_mode && { esp_match_mode: newData.esp_match_mode }),
            ...(newData.max_new_leads_per_day !== campaign.max_new_leads_per_day && {
                max_new_leads_per_day: newData.max_new_leads_per_day,
            }),
            ...(newData.prioritize_new_leads !== campaign.prioritize_new_leads && {
                prioritize_new_leads: newData.prioritize_new_leads,
            }),
            ...(newData.risky_emails !== campaign.risky_emails && { risky_emails: newData.risky_emails }),

            // Advanced — tracking domain
            ...(newData.tracking_domain !== campaign.tracking_domain && {
                tracking_domain: newData.tracking_domain,
            }),

            // Advanced — cc/bcc
            ...(newData.cc !== campaign.cc && { cc: newData.cc }),
            ...(newData.bcc !== campaign.bcc && { bcc: newData.bcc }),

            // Contact order
            ...(newData.contact_order_by !== campaign.contact_order_by && {
                contact_order_by: newData.contact_order_by,
            }),
            ...(newData.contact_order_dir !== campaign.contact_order_dir && {
                contact_order_dir: newData.contact_order_dir,
            }),
            ...(newData.contact_order_field !== campaign.contact_order_field && {
                contact_order_field: newData.contact_order_field,
            }),
        };
    };

    // Whether the explicit sender pool changed. Only meaningful while the
    // campaign is (or is being switched to) the explicit strategy.
    const accountsDirty = React.useMemo(() => {
        if (newData.sender_strategy !== "explicit") return false;
        if (explicitAccounts.length !== savedAccounts.length) return true;
        const a = new Set(savedAccounts);
        return explicitAccounts.some((id) => !a.has(id));
    }, [newData.sender_strategy, explicitAccounts, savedAccounts]);

    const validationError = (): string | null => {
        if (newData.daily_limit < DAILY_MIN || newData.daily_limit > DAILY_MAX) {
            return `Daily limit must be between ${DAILY_MIN} and ${DAILY_MAX}.`;
        }
        if (newData.ramp_enabled && newData.ramp_start > newData.ramp_ceiling) {
            return "Ramp start must be less than or equal to the ramp ceiling.";
        }
        if (newData.sender_strategy === "explicit" && explicitAccounts.length === 0) {
            return "Pick at least one sending account, or switch back to selecting by tag.";
        }
        return null;
    };

    async function submit() {
        if (loading) return;
        const err = validationError();
        if (err) {
            toast.error(err);
            return;
        }
        try {
            setLoading(true);
            const data = getChanges();
            // Persist the explicit sender pool through its own endpoint (it is
            // not part of the campaign PATCH body). Map each picked mailbox to a
            // CampaignSender with weight 1 so volume splits evenly.
            const writeSenders = newData.sender_strategy === "explicit" && accountsDirty;
            await toast.promise(
                (async () => {
                    if (Object.keys(data).length > 0) {
                        await updateCampaign.mutateAsync(data);
                    }
                    if (writeSenders) {
                        await replaceSenders.mutateAsync(
                            explicitAccounts.map((id) => ({ email_account_id: id, weight: 1 })),
                        );
                        setSavedAccounts(explicitAccounts);
                    }
                })(),
                {
                    loading: "Saving…",
                    success: "Campaign successfully updated.",
                    error: (e: AppError) => buildError(e),
                },
            );
        } finally {
            setLoading(false);
        }
    }

    const tabContent: Record<TabKey, React.ReactNode> = {
        standard: (
            <CampaignAppearance
                campaign={campaign}
                newCampaign={newData}
                setNewCampaign={setNewData}
                explicitAccounts={explicitAccounts}
                setExplicitAccounts={setExplicitAccounts}
            />
        ),
        advanced: (
            <CampaignEmails campaign={campaign} newCampaign={newData} setNewCampaign={setNewData} />
        ),
        order: (
            <CampaignContactOrder campaign={campaign} newCampaign={newData} setNewCampaign={setNewData} />
        ),
    };

    const hasChanges = Object.keys(getChanges()).length > 0 || accountsDirty;
    const blocked = validationError() !== null;

    return (
        <div>
            <div className="flex flex-col md:flex-row gap-5">
                <div className="flex md:flex-col gap-0.5 pb-3 md:pb-0 border-b md:border-b-0 md:border-r border-slate-200/60 md:w-48 md:shrink-0 md:pr-4 overflow-x-auto no-scrollbar">
                    {TABS.map(({ key, title, Icon }) => {
                        const active = activeTab === key;
                        return (
                            <button
                                key={key}
                                onClick={() => setActiveTab(key)}
                                className={`h-8 px-2.5 md:w-full inline-flex items-center gap-1.5 text-left text-[12.5px] rounded-md select-none transition-colors shrink-0 ${
                                    active
                                        ? "bg-sky-50 text-sky-700 font-medium"
                                        : "text-slate-500 hover:text-slate-900 hover:bg-slate-50"
                                }`}
                            >
                                <Icon className="w-3.5 h-3.5 shrink-0" />
                                {title}
                            </button>
                        );
                    })}
                </div>
                <div className="flex-1 min-w-0">{tabContent[activeTab]}</div>
            </div>
            <div
                className={`flex items-center justify-end gap-2 mt-6 pt-4 border-t border-slate-200/60 transition-opacity duration-100 ${
                    hasChanges ? "opacity-100" : "opacity-40 pointer-events-none"
                }`}
            >
                {blocked && hasChanges && (
                    <span className="mr-auto text-[11.5px] text-rose-500">{validationError()}</span>
                )}
                <button
                    className="h-7 px-3 text-[12px] font-medium text-slate-600 hover:text-slate-900 border border-slate-200 hover:border-slate-300 rounded-md transition-colors"
                    onClick={() => {
                        setNewData(campaign);
                        setExplicitAccounts(savedAccounts);
                    }}
                >
                    Reset
                </button>
                <button
                    className="h-7 px-3 bg-sky-600 hover:bg-sky-700 text-white rounded-md text-[12px] font-medium transition-colors min-w-[110px] inline-flex items-center justify-center disabled:opacity-60"
                    onClick={submit}
                    disabled={blocked}
                >
                    {loading ? <Loading className="h-4" /> : "Save changes"}
                </button>
            </div>
        </div>
    );
}
