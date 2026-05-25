// Contact 360 slide-over.
//
// Panel chrome:
//   - 34rem wide right-side panel with a hairline-bordered card feel
//   - Hero header: gradient avatar, display name, email + copy,
//     subscribed/suppressed status chip, company chip, close
//   - Segmented tab strip with icons + animated indicator
//     (framer-motion layoutId)
//   - One scroll container per tab
//   - Footer (Discard / Save) is only attached on the Details tab,
//     since the other tabs are read-only or have their own affordances
//
// Tabs:
//   - Overview  → engagement stats + suppression + profile snapshot
//   - Activity  → merged timeline
//   - Notes     → CRM notes CRUD
//   - Details   → identity / categories / campaigns / custom fields

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    BanIcon,
    BuildingIcon,
    CheckIcon,
    CopyIcon,
    Loader2Icon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import useUpdateContact from "@/lib/api/hooks/app/contacts/useUpdateContact";
import useContact from "@/lib/api/hooks/app/contacts/useContact";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import OverviewTab from "./contact-edit/OverviewTab";
import ActivityTab from "./contact-edit/ActivityTab";
import NotesTab from "./contact-edit/NotesTab";
import DetailsTab, { type CustomField } from "./contact-edit/DetailsTab";
import {
    CONTACT_SLIDE_TABS,
    type ContactSlideTab,
} from "./contact-edit/tabs";

export default function ContactEdit({
    contacts,
    active,
    setActive,
}: {
    contacts: Contact[];
    active: string;
    setActive: React.Dispatch<React.SetStateAction<string>>;
}) {
    const contact = React.useMemo(
        () => contacts.find((c) => c.id === active),
        [contacts, active],
    );

    return (
        <AnimatePresence>
            {contact && (
                <ContactEditPanel
                    key={contact.id}
                    contact={contact}
                    onClose={() => setActive("")}
                />
            )}
        </AnimatePresence>
    );
}

function ContactEditPanel({
    contact,
    onClose,
}: {
    contact: Contact;
    onClose: () => void;
}) {
    const update = useUpdateContact(contact.id);

    // Hydrated detail used by Overview. Fetched on open; the Details
    // tab still works off the list-row Contact so it's editable
    // immediately even before /detail resolves.
    const detail = useContact(contact.id);

    const [tab, setTab] = React.useState<ContactSlideTab>("overview");

    const [firstName, setFirstName] = React.useState(contact.first_name);
    const [lastName, setLastName] = React.useState(contact.last_name);
    const [email, setEmail] = React.useState(contact.email);
    const [company, setCompany] = React.useState(contact.company);
    const [phone, setPhone] = React.useState(contact.phone);
    const [subscribed, setSubscribed] = React.useState(contact.subscribed);
    const [campaigns, setCampaigns] = React.useState<MiniCampaign[]>(contact.campaigns ?? []);
    const [categoryIds, setCategoryIds] = React.useState<string[]>(
        () => (contact.categories ?? []).map((c) => c.id),
    );
    const [customFields, setCustomFields] = React.useState<CustomField[]>(() =>
        Object.entries(contact.custom_fields ?? {}).map(([n, v]) => ({ name: n, value: v })),
    );

    function reset() {
        setFirstName(contact.first_name);
        setLastName(contact.last_name);
        setEmail(contact.email);
        setCompany(contact.company);
        setPhone(contact.phone);
        setSubscribed(contact.subscribed);
        setCampaigns(contact.campaigns ?? []);
        setCategoryIds((contact.categories ?? []).map((c) => c.id));
        setCustomFields(Object.entries(contact.custom_fields ?? {}).map(([n, v]) => ({ name: n, value: v })));
    }

    const recordFromCF = React.useCallback((fields: CustomField[]) => {
        const out: Record<string, string> = {};
        for (const f of fields) {
            if (!f.name.trim()) continue;
            out[f.name.trim()] = f.value;
        }
        return out;
    }, []);

    const dirty = React.useMemo(() => {
        if (firstName !== contact.first_name) return true;
        if (lastName !== contact.last_name) return true;
        if (email !== contact.email) return true;
        if (company !== contact.company) return true;
        if (phone !== contact.phone) return true;
        if (subscribed !== contact.subscribed) return true;
        if (JSON.stringify(recordFromCF(customFields)) !== JSON.stringify(contact.custom_fields ?? {})) return true;
        const curC = new Set(contact.campaigns.map((c) => c.id));
        const nextC = new Set(campaigns.map((c) => c.id));
        if (curC.size !== nextC.size) return true;
        for (const id of curC) if (!nextC.has(id)) return true;
        const curCat = new Set((contact.categories ?? []).map((c) => c.id));
        const nextCat = new Set(categoryIds);
        if (curCat.size !== nextCat.size) return true;
        for (const id of curCat) if (!nextCat.has(id)) return true;
        return false;
    }, [contact, firstName, lastName, email, company, phone, subscribed, customFields, campaigns, categoryIds, recordFromCF]);

    async function save() {
        if (!dirty) return;
        const data: Record<string, unknown> = {};
        if (firstName !== contact.first_name) data.first_name = firstName;
        if (lastName !== contact.last_name) data.last_name = lastName;
        if (email !== contact.email) data.email = email;
        if (company !== contact.company) data.company = company;
        if (phone !== contact.phone) data.phone = phone;
        if (subscribed !== contact.subscribed) data.subscribed = subscribed;
        const cf = recordFromCF(customFields);
        if (JSON.stringify(cf) !== JSON.stringify(contact.custom_fields ?? {})) data.custom_fields = cf;
        const cur = new Set(contact.campaigns.map((c) => c.id));
        const next = new Set(campaigns.map((c) => c.id));
        let campaignsChanged = cur.size !== next.size;
        if (!campaignsChanged) for (const id of cur) if (!next.has(id)) { campaignsChanged = true; break; }
        if (campaignsChanged) data.campaigns = campaigns.map((c) => c.id);

        const curCat = new Set((contact.categories ?? []).map((c) => c.id));
        const nextCat = new Set(categoryIds);
        let categoriesChanged = curCat.size !== nextCat.size;
        if (!categoriesChanged) for (const id of curCat) if (!nextCat.has(id)) { categoriesChanged = true; break; }
        if (categoriesChanged) data.categories = categoryIds;

        try {
            await toast.promise(update.mutateAsync(data), {
                loading: "Updating contact…",
                success: "Contact updated",
                error: (err: AppError) => buildError(err),
            });
            onClose();
        } catch {
            /* toast surfaced */
        }
    }

    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key === "Escape") {
                if (dirty && !window.confirm("Discard unsaved changes?")) return;
                onClose();
            }
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [dirty, onClose]);

    const displayName =
        firstName || lastName ? `${firstName} ${lastName}`.trim() : "Unnamed contact";
    const suppressed = !!detail.data?.suppression;

    return (
        <motion.div
            key="overlay"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-[110] flex justify-end bg-slate-900/40 backdrop-blur-[3px]"
            onMouseDown={onClose}
        >
            <motion.aside
                key="panel"
                initial={{ x: 32, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                exit={{ x: 32, opacity: 0 }}
                transition={{ duration: 0.22, ease: [0.32, 0.72, 0, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="flex flex-col w-[34rem] max-w-[95%] h-full bg-white border-l border-slate-200 shadow-[-16px_0_32px_-16px_rgba(15,23,42,0.18)]"
            >
                <ContactHeader
                    contact={contact}
                    displayName={displayName}
                    subscribed={subscribed}
                    suppressed={suppressed}
                    dirty={dirty && tab === "details"}
                    onClose={() =>
                        dirty && !window.confirm("Discard unsaved changes?")
                            ? undefined
                            : onClose()
                    }
                />

                <TabStrip tab={tab} setTab={setTab} />

                <div className="flex-1 min-h-0 overflow-y-auto px-5 py-5 bg-slate-50/30">
                    {tab === "overview" && (
                        <OverviewTab
                            contact={contact}
                            detail={detail.data}
                            detailLoading={detail.isLoading}
                        />
                    )}
                    {tab === "activity" && <ActivityTab contactId={contact.id} />}
                    {tab === "notes" && <NotesTab contactId={contact.id} />}
                    {tab === "details" && (
                        <DetailsTab
                            contact={contact}
                            firstName={firstName}
                            setFirstName={setFirstName}
                            lastName={lastName}
                            setLastName={setLastName}
                            email={email}
                            setEmail={setEmail}
                            company={company}
                            setCompany={setCompany}
                            phone={phone}
                            setPhone={setPhone}
                            subscribed={subscribed}
                            setSubscribed={setSubscribed}
                            campaigns={campaigns}
                            setCampaigns={setCampaigns}
                            categoryIds={categoryIds}
                            setCategoryIds={setCategoryIds}
                            customFields={customFields}
                            setCustomFields={setCustomFields}
                        />
                    )}
                </div>

                {tab === "details" && (
                    <footer className="h-14 px-4 border-t border-slate-200 flex items-center gap-2 shrink-0 bg-white">
                        <span className="text-[11px] text-slate-400">
                            {dirty ? "You have unsaved changes" : "All changes saved"}
                        </span>
                        <button
                            type="button"
                            onClick={reset}
                            disabled={!dirty}
                            className="ml-auto h-8 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
                        >
                            Discard
                        </button>
                        <button
                            type="button"
                            onClick={save}
                            disabled={!dirty || update.isPending}
                            className="h-8 px-3.5 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 shadow-sm"
                        >
                            {update.isPending ? (
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                                <CheckIcon className="w-3.5 h-3.5" />
                            )}
                            Save changes
                        </button>
                    </footer>
                )}
            </motion.aside>
        </motion.div>
    );
}

function ContactHeader({
    contact,
    displayName,
    subscribed,
    suppressed,
    dirty,
    onClose,
}: {
    contact: Contact;
    displayName: string;
    subscribed: boolean;
    suppressed: boolean;
    dirty: boolean;
    onClose: () => void;
}) {
    const [copied, setCopied] = React.useState(false);
    function copy() {
        navigator.clipboard.writeText(contact.email).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
        });
    }
    const grad = gradientFor(contact.email);
    const initials = initialsFrom(contact, displayName);

    return (
        <header className="relative shrink-0 bg-white border-b border-slate-200">
            <div
                className="absolute inset-x-0 top-0 h-12 opacity-[0.07]"
                style={{ background: grad }}
            />
            <div className="relative px-5 pt-4 pb-3 flex items-start gap-3.5">
                <div
                    className="size-11 rounded-xl flex items-center justify-center text-[13px] font-semibold text-white shrink-0 shadow-sm ring-1 ring-black/5"
                    style={{ backgroundImage: grad }}
                >
                    {initials}
                </div>
                <div className="min-w-0 flex-1 pt-0.5">
                    <div className="flex items-center gap-2">
                        <h2 className="text-[14.5px] font-semibold text-slate-900 truncate leading-tight">
                            {displayName}
                        </h2>
                        {dirty && (
                            <span className="text-[9.5px] uppercase tracking-[0.14em] text-amber-700 bg-amber-50 px-1.5 py-0.5 rounded font-semibold border border-amber-200/80 shrink-0">
                                Unsaved
                            </span>
                        )}
                    </div>
                    <div className="flex items-center gap-1.5 mt-1 min-w-0">
                        <span className="text-[11.5px] text-slate-500 font-mono truncate">
                            {contact.email}
                        </span>
                        <button
                            type="button"
                            onClick={copy}
                            aria-label="Copy email"
                            className="shrink-0 size-5 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                        >
                            {copied ? (
                                <CheckIcon className="w-3 h-3 text-emerald-600" />
                            ) : (
                                <CopyIcon className="w-3 h-3" />
                            )}
                        </button>
                    </div>
                    <div className="flex items-center gap-1.5 mt-2 flex-wrap">
                        {suppressed ? (
                            <Chip tone="red" icon={<BanIcon className="w-2.5 h-2.5" />}>
                                Suppressed
                            </Chip>
                        ) : subscribed ? (
                            <Chip tone="emerald" dot>
                                Subscribed
                            </Chip>
                        ) : (
                            <Chip tone="slate" dot>
                                Unsubscribed
                            </Chip>
                        )}
                        {contact.company && (
                            <Chip tone="slate" icon={<BuildingIcon className="w-2.5 h-2.5" />}>
                                {contact.company}
                            </Chip>
                        )}
                        {contact.categories?.slice(0, 2).map((c) => (
                            <span
                                key={c.id}
                                className="inline-flex h-5 items-center px-1.5 rounded text-[10.5px] font-medium"
                                style={{
                                    backgroundColor: `${c.color}1a`,
                                    color: c.color,
                                }}
                            >
                                {c.title}
                            </span>
                        ))}
                        {contact.categories && contact.categories.length > 2 && (
                            <span className="text-[10.5px] text-slate-400">
                                +{contact.categories.length - 2}
                            </span>
                        )}
                    </div>
                </div>
                <button
                    type="button"
                    onClick={onClose}
                    aria-label="Close"
                    className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>
        </header>
    );
}

function TabStrip({
    tab,
    setTab,
}: {
    tab: ContactSlideTab;
    setTab: (t: ContactSlideTab) => void;
}) {
    return (
        <nav className="px-4 py-2 border-b border-slate-200 shrink-0 bg-white">
            <div className="inline-flex bg-slate-100/80 rounded-lg p-0.5 gap-0.5">
                {CONTACT_SLIDE_TABS.map((t) => {
                    const isActive = tab === t.id;
                    const Icon = t.icon;
                    return (
                        <button
                            key={t.id}
                            type="button"
                            onClick={() => setTab(t.id)}
                            className="relative h-7 px-2.5 rounded-md text-[11.5px] font-medium inline-flex items-center gap-1.5 outline-none"
                        >
                            {isActive && (
                                <motion.div
                                    layoutId="contact-tab-bg"
                                    className="absolute inset-0 rounded-md bg-white shadow-sm border border-slate-200/90"
                                    transition={{ type: "spring", duration: 0.35, bounce: 0.15 }}
                                />
                            )}
                            <span
                                className={`relative z-10 inline-flex items-center gap-1.5 transition-colors ${
                                    isActive
                                        ? "text-slate-900"
                                        : "text-slate-500 hover:text-slate-800"
                                }`}
                            >
                                <Icon className="w-3.5 h-3.5" />
                                {t.label}
                            </span>
                        </button>
                    );
                })}
            </div>
        </nav>
    );
}

function Chip({
    tone,
    icon,
    dot,
    children,
}: {
    tone: "emerald" | "red" | "slate";
    icon?: React.ReactNode;
    dot?: boolean;
    children: React.ReactNode;
}) {
    const tones = {
        emerald: "bg-emerald-50 text-emerald-700 border-emerald-200/80",
        red: "bg-red-50 text-red-700 border-red-200/80",
        slate: "bg-slate-50 text-slate-600 border-slate-200/80",
    } as const;
    const dotTone = {
        emerald: "bg-emerald-500",
        red: "bg-red-500",
        slate: "bg-slate-400",
    } as const;
    return (
        <span
            className={`inline-flex items-center gap-1 h-5 px-1.5 rounded text-[10.5px] font-medium border ${tones[tone]}`}
        >
            {dot && (
                <span className={`size-1.5 rounded-full ${dotTone[tone]}`} />
            )}
            {icon}
            {children}
        </span>
    );
}

// Deterministic gradient avatar — same email always renders the same
// background so the avatar reads as identity, not noise.
function gradientFor(seed: string): string {
    const palette: [string, string][] = [
        ["#0ea5e9", "#6366f1"], // sky → indigo
        ["#8b5cf6", "#ec4899"], // violet → pink
        ["#10b981", "#0ea5e9"], // emerald → sky
        ["#f59e0b", "#ef4444"], // amber → red
        ["#06b6d4", "#3b82f6"], // cyan → blue
        ["#ec4899", "#f43f5e"], // pink → rose
        ["#6366f1", "#8b5cf6"], // indigo → violet
        ["#14b8a6", "#22c55e"], // teal → green
    ];
    let h = 0;
    for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
    const [a, b] = palette[h % palette.length];
    return `linear-gradient(135deg, ${a} 0%, ${b} 100%)`;
}

function initialsFrom(contact: Contact, displayName: string): string {
    const first = (contact.first_name || "").trim();
    const last = (contact.last_name || "").trim();
    if (first || last) {
        return `${first.charAt(0) || ""}${last.charAt(0) || ""}`.toUpperCase() || "?";
    }
    if (displayName && displayName !== "Unnamed contact") {
        const parts = displayName.split(/\s+/).filter(Boolean);
        return ((parts[0]?.charAt(0) || "") + (parts[1]?.charAt(0) || "")).toUpperCase() || "?";
    }
    const e = (contact.email || "").trim();
    return e.slice(0, 2).toUpperCase() || "?";
}
