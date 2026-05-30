// Contact 360 slide-over.
//
// Panel chrome:
//   - 32rem wide right-side panel
//   - Header: solid round avatar, display name, email + copy, single
//     status pill when something is off (suppressed/unsubscribed),
//     close button
//   - Segmented tab strip with animated indicator
//   - One scroll container per tab
//   - Footer (Discard / Save) only on Details tab
//
// Tabs:
//   - Overview  → engagement stats + suppression + profile snapshot
//   - Activity  → merged timeline
//   - Notes     → CRM notes CRUD
//   - Details   → identity / categories / campaigns / custom fields

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import { CheckIcon, CopyIcon, Loader2Icon, XIcon } from "lucide-react";
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
            className="fixed inset-0 z-[110] flex justify-end bg-slate-900/30 backdrop-blur-[2px]"
            onMouseDown={onClose}
        >
            <motion.aside
                key="panel"
                initial={{ x: 32, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                exit={{ x: 32, opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="flex flex-col w-[32rem] max-w-[95%] h-full bg-white border-l border-slate-200 shadow-[-12px_0_24px_-12px_rgba(15,23,42,0.08)]"
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

                <div className="flex-1 min-h-0 overflow-y-auto px-5 py-5">
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
                    <footer className="h-12 px-3 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-white">
                        <button
                            type="button"
                            onClick={reset}
                            disabled={!dirty}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900 hover:bg-slate-100 transition-colors disabled:opacity-40 disabled:hover:bg-transparent"
                        >
                            Discard
                        </button>
                        <button
                            type="button"
                            onClick={save}
                            disabled={!dirty || update.isPending}
                            className="ml-auto h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50"
                        >
                            {update.isPending ? (
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                            ) : (
                                <CheckIcon className="w-3 h-3" />
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
    const initials = initialsFrom(contact, displayName);

    // Only show a status pill when something is wrong; subscribed-and-OK
    // is the default and doesn't need a chip in the chrome.
    let statusPill: React.ReactNode = null;
    if (suppressed) {
        statusPill = (
            <span className="inline-flex h-5 items-center px-1.5 rounded text-[10px] font-medium text-red-700 bg-red-50 border border-red-200">
                Suppressed
            </span>
        );
    } else if (!subscribed) {
        statusPill = (
            <span className="inline-flex h-5 items-center px-1.5 rounded text-[10px] font-medium text-slate-600 bg-slate-100 border border-slate-200">
                Unsubscribed
            </span>
        );
    }

    return (
        <header className="px-4 pt-4 pb-3 border-b border-slate-200 flex items-start gap-3 shrink-0">
            <div className="size-9 rounded-full bg-slate-100 flex items-center justify-center text-[12px] font-semibold text-slate-700 shrink-0">
                {initials}
            </div>
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 min-w-0">
                    <h2 className="text-[14px] font-semibold text-slate-900 truncate leading-tight">
                        {displayName}
                    </h2>
                    {statusPill}
                    {dirty && (
                        <span className="inline-flex h-5 items-center px-1.5 rounded text-[10px] font-medium text-amber-700 bg-amber-50 border border-amber-200">
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
            </div>
            <button
                type="button"
                onClick={onClose}
                aria-label="Close"
                className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
            >
                <XIcon className="w-3.5 h-3.5" />
            </button>
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
        <nav className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200">
            {CONTACT_SLIDE_TABS.map((t) => {
                const isActive = tab === t.id;
                return (
                    <button
                        key={t.id}
                        type="button"
                        onClick={() => setTab(t.id)}
                        className={`relative h-10 px-2.5 inline-flex items-center gap-1.5 text-[12.5px] outline-none transition-colors ${
                            isActive
                                ? "text-slate-900 font-medium"
                                : "text-slate-500 hover:text-slate-800"
                        }`}
                    >
                        <t.icon className="w-3.5 h-3.5" />
                        {t.label}
                        {isActive && (
                            <motion.span
                                layoutId="contact-tab-underline"
                                className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                                transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                            />
                        )}
                    </button>
                );
            })}
        </nav>
    );
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
