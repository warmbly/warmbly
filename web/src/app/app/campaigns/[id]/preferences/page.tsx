import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Loading } from "@/components/loader";
import { useCampaign } from "@/hooks/context/campaign";
import type Campaign from "@/lib/api/models/app/campaigns/Campaign";
import {
    DeliverabilitySection,
    GeneralSection,
    SendingAccountsSection,
} from "@/components/app/campaigns/preferences/CampaignAppearance";
import {
    CcBccSection,
    EspMatchingSection,
    LeadFlowSection,
    RotationRampSection,
} from "@/components/app/campaigns/preferences/CampaignEmails";
import CampaignContactOrder from "@/components/app/campaigns/preferences/CampaignContactOrder";
import CampaignFolderField from "@/components/app/campaigns/CampaignFolderField";
import useUpdateCampaign from "@/lib/api/hooks/app/campaigns/useUpdateCampaign";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useCampaignSenders from "@/lib/api/hooks/app/campaigns/useCampaignSenders";
import useReplaceCampaignSenders from "@/lib/api/hooks/app/campaigns/useReplaceCampaignSenders";

const DAILY_MIN = 3;
const DAILY_MAX = 100;

// One scrolling page — every section stacks in order and the left nav is a
// scrollspy over these ids.
const SECTIONS = [
    { id: "general", label: "General", description: "Name and description for this campaign." },
    { id: "folders", label: "Folders", description: "Organize this campaign into folders." },
    {
        id: "senders",
        label: "Sending accounts",
        description: "Which mailboxes send this campaign — by tag, individually, or both — and the per-mailbox daily cap.",
    },
    {
        id: "deliverability",
        label: "Deliverability",
        description: "Reply handling, open/link tracking, and the unsubscribe header.",
    },
    {
        id: "rotation",
        label: "Rotation & ramp-up",
        description: "How volume is distributed across mailboxes and ramped over time.",
    },
    {
        id: "matching",
        label: "ESP matching",
        description: "Align the sending mailbox provider with each recipient's provider.",
    },
    {
        id: "leadflow",
        label: "Lead flow",
        description: "New-lead throttle, prioritization, and risky-address policy.",
    },
    {
        id: "ccbcc",
        label: "CC & BCC",
        description: "Copy extra addresses on every email sent by this campaign.",
    },
    { id: "order", label: "Contact order", description: "The order contacts are sent in." },
] as const;

type SectionId = (typeof SECTIONS)[number]["id"];

// Walk up from an element to the nearest scrollable ancestor (the app shell
// scrolls an inner overflow-auto container, not the window).
function findScrollParent(el: HTMLElement | null): HTMLElement | Window {
    let node = el?.parentElement ?? null;
    while (node) {
        const oy = getComputedStyle(node).overflowY;
        if ((oy === "auto" || oy === "scroll") && node.scrollHeight > node.clientHeight) return node;
        node = node.parentElement;
    }
    return window;
}

// useScrollSpy — the active section is the LAST one whose top has scrolled above
// a reading line near the top of the viewport. Because the sections are ordered,
// this also correctly highlights the final section once you reach the bottom
// (where a thin-band observer would never fire). A short click-lock keeps a nav
// click's smooth scroll from flickering the highlight mid-animation.
function useScrollSpy(ids: readonly string[]) {
    const [activeId, setActiveId] = React.useState<string>(ids[0]);
    const lockUntilRef = React.useRef(0);

    React.useEffect(() => {
        const READING_LINE = 140; // px from the top of the viewport
        const first = document.getElementById(ids[0]);
        const target = findScrollParent(first);

        const compute = () => {
            if (Date.now() < lockUntilRef.current) return;
            let current = ids[0];
            for (const id of ids) {
                const el = document.getElementById(id);
                if (!el) continue;
                if (el.getBoundingClientRect().top - READING_LINE <= 0) current = id;
                else break;
            }
            setActiveId(current);
        };

        let raf = 0;
        const onScroll = () => {
            cancelAnimationFrame(raf);
            raf = requestAnimationFrame(compute);
        };
        compute();
        target.addEventListener("scroll", onScroll, { passive: true });
        window.addEventListener("resize", onScroll);
        return () => {
            cancelAnimationFrame(raf);
            target.removeEventListener("scroll", onScroll);
            window.removeEventListener("resize", onScroll);
        };
    }, [ids]);

    const scrollTo = React.useCallback((id: string) => {
        setActiveId(id);
        lockUntilRef.current = Date.now() + 700;
        document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" });
    }, []);

    return { activeId, scrollTo };
}

// Order-independent set equality for id arrays. Toggling a folder/tag on then
// off produces a new array reference with the same contents — a plain `!==`
// would flag that as a phantom unsaved change, so compare by membership.
function sameIdSet(a: string[] = [], b: string[] = []): boolean {
    if (a.length !== b.length) return false;
    const set = new Set(a);
    return b.every((id) => set.has(id));
}

export default function CampaignPreferences() {
    const campaign = useCampaign();
    if (!campaign) {
        throw new Error("CampaignPreferences cannot be rendered without a campaign");
    }

    const updateCampaign = useUpdateCampaign(campaign.id);
    const replaceSenders = useReplaceCampaignSenders(campaign.id);

    // The explicit sender pool is managed by the senders endpoint (not PATCH).
    // Always loaded: the picker mixes tags + specific mailboxes in one control,
    // so we need the current explicit pool regardless of how senders were chosen.
    const { data: senders } = useCampaignSenders(campaign.id, true);

    const [loading, setLoading] = React.useState(false);
    const [newData, setNewData] = React.useState<Campaign>(campaign);

    // Selected mailbox ids for individually-picked senders + the saved baseline
    // we diff against. Seeded from the campaign's current senders.
    const [explicitAccounts, setExplicitAccounts] = React.useState<string[]>([]);
    const [savedAccounts, setSavedAccounts] = React.useState<string[]>([]);

    const ids = React.useMemo(() => SECTIONS.map((s) => s.id), []);
    const { activeId, scrollTo } = useScrollSpy(ids);

    React.useEffect(() => {
        if (!campaign) return;
        setNewData(campaign);
    }, [campaign]);

    React.useEffect(() => {
        if (!senders) return;
        const accountIds = senders.map((s) => s.email_account_id);
        setExplicitAccounts(accountIds);
        setSavedAccounts(accountIds);
    }, [senders]);

    const getChanges = (): Partial<Campaign> => {
        if (!newData) return {};
        return {
            ...(newData.name !== campaign.name && { name: newData.name }),
            ...(newData.description !== campaign.description && { description: newData.description }),
            ...(!sameIdSet(newData.folders ?? [], campaign.folders ?? []) && {
                folders: newData.folders ?? [],
            }),

            // Sending + deliverability
            ...(!sameIdSet(newData.email_tags ?? [], campaign.email_tags ?? []) && {
                email_tags: newData.email_tags,
            }),
            ...(newData.daily_limit !== campaign.daily_limit && { daily_limit: newData.daily_limit }),
            ...(newData.stop_on_reply !== campaign.stop_on_reply && { stop_on_reply: newData.stop_on_reply }),
            ...(newData.text_only !== campaign.text_only && { text_only: newData.text_only }),
            ...(newData.open_tracking !== campaign.open_tracking && { open_tracking: newData.open_tracking }),
            ...(newData.link_tracking !== campaign.link_tracking && { link_tracking: newData.link_tracking }),
            ...(newData.unsubscribe_header !== campaign.unsubscribe_header && {
                unsubscribe_header: newData.unsubscribe_header,
            }),

            // Rotation
            ...(newData.rotation_mode !== campaign.rotation_mode && { rotation_mode: newData.rotation_mode }),

            // Ramp-up
            ...(newData.ramp_enabled !== campaign.ramp_enabled && { ramp_enabled: newData.ramp_enabled }),
            ...(newData.ramp_start !== campaign.ramp_start && { ramp_start: newData.ramp_start }),
            ...(newData.ramp_increment !== campaign.ramp_increment && { ramp_increment: newData.ramp_increment }),
            ...(newData.ramp_ceiling !== campaign.ramp_ceiling && { ramp_ceiling: newData.ramp_ceiling }),

            // ESP matching + new-lead throttle
            ...(newData.esp_match_mode !== campaign.esp_match_mode && { esp_match_mode: newData.esp_match_mode }),
            ...(newData.max_new_leads_per_day !== campaign.max_new_leads_per_day && {
                max_new_leads_per_day: newData.max_new_leads_per_day,
            }),
            ...(newData.prioritize_new_leads !== campaign.prioritize_new_leads && {
                prioritize_new_leads: newData.prioritize_new_leads,
            }),
            ...(newData.risky_emails !== campaign.risky_emails && { risky_emails: newData.risky_emails }),

            // cc/bcc
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

    // Whether the explicit sender pool changed (mailboxes picked individually).
    // Tags are diffed separately via getChanges → email_tags.
    const accountsDirty = React.useMemo(() => {
        if (explicitAccounts.length !== savedAccounts.length) return true;
        const a = new Set(savedAccounts);
        return explicitAccounts.some((id) => !a.has(id));
    }, [explicitAccounts, savedAccounts]);

    const validationError = (): string | null => {
        if (newData.daily_limit < DAILY_MIN || newData.daily_limit > DAILY_MAX) {
            return `Daily limit must be between ${DAILY_MIN} and ${DAILY_MAX}.`;
        }
        if (newData.ramp_enabled && newData.ramp_start > newData.ramp_ceiling) {
            return "Ramp start must be less than or equal to the ramp ceiling.";
        }
        // Sending accounts: nothing selected is valid — it means "all active
        // mailboxes", so there is no minimum-selection requirement anymore.
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
            // CampaignSender with weight 1 so volume splits evenly. An empty list
            // clears the explicit pool (falling back to tags / all mailboxes).
            const writeSenders = accountsDirty;
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

    const renderSection = (id: SectionId): React.ReactNode => {
        switch (id) {
            case "general":
                return <GeneralSection campaign={campaign} newCampaign={newData} setNewCampaign={setNewData} />;
            case "folders":
                return (
                    <CampaignFolderField
                        selected={newData.folders ?? []}
                        onToggle={(id) =>
                            setNewData({
                                ...newData,
                                folders: (newData.folders ?? []).includes(id)
                                    ? (newData.folders ?? []).filter((x) => x !== id)
                                    : [...(newData.folders ?? []), id],
                            })
                        }
                    />
                );
            case "senders":
                return (
                    <SendingAccountsSection
                        newCampaign={newData}
                        setNewCampaign={setNewData}
                        explicitAccounts={explicitAccounts}
                        setExplicitAccounts={setExplicitAccounts}
                    />
                );
            case "deliverability":
                return <DeliverabilitySection newCampaign={newData} setNewCampaign={setNewData} />;
            case "rotation":
                return <RotationRampSection newCampaign={newData} setNewCampaign={setNewData} />;
            case "matching":
                return (
                    <EspMatchingSection
                        newCampaign={newData}
                        setNewCampaign={setNewData}
                        explicitAccounts={explicitAccounts}
                    />
                );
            case "leadflow":
                return <LeadFlowSection newCampaign={newData} setNewCampaign={setNewData} />;
            case "ccbcc":
                return <CcBccSection newCampaign={newData} setNewCampaign={setNewData} />;
            case "order":
                return <CampaignContactOrder campaign={campaign} newCampaign={newData} setNewCampaign={setNewData} />;
        }
    };

    const hasChanges = Object.keys(getChanges()).length > 0 || accountsDirty;
    const blocked = validationError() !== null;

    return (
        <div className="flex flex-col md:flex-row gap-6 lg:gap-10">
            {/* Scrollspy nav */}
            <nav className="md:w-56 md:shrink-0">
                <div className="md:sticky md:top-1 z-10 sticky top-0 -mx-5 px-5 md:mx-0 md:px-0 bg-white/90 backdrop-blur md:bg-transparent md:backdrop-blur-0">
                    <p className="hidden md:block px-2.5 mb-2 text-[10px] uppercase tracking-[0.14em] text-slate-400">
                        On this page
                    </p>
                    <div className="flex md:flex-col gap-0.5 overflow-x-auto no-scrollbar py-2 md:py-0 border-b md:border-0 border-slate-200/60">
                        {SECTIONS.map(({ id, label }) => {
                            const active = activeId === id;
                            return (
                                <button
                                    key={id}
                                    type="button"
                                    onClick={() => scrollTo(id)}
                                    className={`relative h-8 px-2.5 md:w-full inline-flex items-center text-left text-[12.5px] rounded-md select-none transition-colors shrink-0 ${
                                        active
                                            ? "text-sky-700 font-medium"
                                            : "text-slate-500 hover:text-slate-900 hover:bg-slate-50"
                                    }`}
                                >
                                    {active && (
                                        <motion.span
                                            layoutId="settings-nav-active"
                                            className="absolute inset-0 rounded-md bg-sky-50"
                                            transition={{ type: "spring", duration: 0.35, bounce: 0.15 }}
                                        />
                                    )}
                                    <span className="relative z-10 whitespace-nowrap">{label}</span>
                                </button>
                            );
                        })}
                    </div>
                </div>
            </nav>

            {/* Stacked sections */}
            <div className="flex-1 min-w-0">
                <div className="divide-y divide-slate-200/60">
                    {SECTIONS.map(({ id, label, description }) => (
                        <section key={id} id={id} className="scroll-mt-24 md:scroll-mt-6 py-7 first:pt-0">
                            <div className="mb-4">
                                <h2 className="text-[13.5px] font-semibold text-slate-900">{label}</h2>
                                {description && (
                                    <p className="text-[11.5px] text-slate-400 mt-1 leading-relaxed">{description}</p>
                                )}
                            </div>
                            {renderSection(id)}
                        </section>
                    ))}
                </div>

                {/* Floating save bar — only present when there are unsaved changes. */}
                <AnimatePresence>
                    {hasChanges && (
                        <motion.div
                            initial={{ y: 24, opacity: 0 }}
                            animate={{ y: 0, opacity: 1 }}
                            exit={{ y: 24, opacity: 0 }}
                            transition={{ type: "spring", duration: 0.3, bounce: 0.2 }}
                            className="sticky bottom-0 z-20 mt-2 py-3 bg-white/90 backdrop-blur border-t border-slate-200/70 flex items-center justify-end gap-2"
                        >
                            {blocked && (
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
                        </motion.div>
                    )}
                </AnimatePresence>
            </div>
        </div>
    );
}
