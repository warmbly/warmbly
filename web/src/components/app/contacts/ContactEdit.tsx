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
import { CalendarPlusIcon, CheckIcon, CopyIcon, Loader2Icon, MailIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast";
import useUpdateContact from "@/lib/api/hooks/app/contacts/useUpdateContact";
import useContact from "@/lib/api/hooks/app/contacts/useContact";
import { useConfirm } from "@/hooks/context/confirm";
import { useComposeStore } from "@/hooks/useComposeStore";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import BookACallButton from "@/components/app/integrations/BookACallButton";
import ResourceViewers from "@/components/app/presence/ResourceViewers";
import { usePresenceResource } from "@/hooks/PresenceProvider";
import NewMeetingDialog from "@/components/app/meetings/NewMeetingDialog";
import OverviewTab from "./contact-edit/OverviewTab";
import ActivityTab from "./contact-edit/ActivityTab";
import NotesTab from "./contact-edit/NotesTab";
import ResearchTab from "./contact-edit/ResearchTab";
import DetailsTab, { type CustomField } from "./contact-edit/DetailsTab";
import {
    fieldsOf,
    hasUnnamedValue,
    idsOf,
    rebase,
    recordFromCF,
    sameCampaigns,
    sameFields,
    sameIDs,
    sameRows,
} from "./contact-edit/rebase";
import {
    CONTACT_SLIDE_TABS,
    type ContactSlideTab,
} from "./contact-edit/tabs";

export default function ContactEdit({
    contacts,
    active,
    setActive,
    initialTab,
}: {
    contacts: Contact[];
    active: string;
    setActive: React.Dispatch<React.SetStateAction<string>>;
    initialTab?: ContactSlideTab;
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
                    initialTab={initialTab}
                />
            )}
        </AnimatePresence>
    );
}

function ContactEditPanel({
    contact,
    onClose,
    initialTab,
}: {
    contact: Contact;
    onClose: () => void;
    initialTab?: ContactSlideTab;
}) {
    const update = useUpdateContact(contact.id);
    const detail = useContact(contact.id);
    const confirm = useConfirm();

    // Collaboration: claim this contact while the 360 panel is open so a
    // teammate editing the same person sees the live-viewer pill up top.
    usePresenceResource(`contact:${contact.id}`, "editing");

    const [tab, setTab] = React.useState<ContactSlideTab>(initialTab ?? "overview");

    const [firstName, setFirstName] = React.useState(contact.first_name);
    const [lastName, setLastName] = React.useState(contact.last_name);
    const [email, setEmail] = React.useState(contact.email);
    const [company, setCompany] = React.useState(contact.company);
    const [phone, setPhone] = React.useState(contact.phone);
    const [subscribed, setSubscribed] = React.useState(contact.subscribed);
    const [campaigns, setCampaigns] = React.useState<MiniCampaign[]>(contact.campaigns ?? []);
    const [categoryIds, setCategoryIds] = React.useState<string[]>(() => idsOf(contact.categories ?? []));
    const [customFields, setCustomFields] = React.useState<CustomField[]>(() =>
        fieldsOf(contact.custom_fields),
    );

    function reset() {
        setFirstName(contact.first_name);
        setLastName(contact.last_name);
        setEmail(contact.email);
        setCompany(contact.company);
        setPhone(contact.phone);
        setSubscribed(contact.subscribed);
        setCampaigns(contact.campaigns ?? []);
        setCategoryIds(idsOf(contact.categories ?? []));
        setCustomFields(fieldsOf(contact.custom_fields));
    }

    // The panel outlives the record it edits: lifting a suppression on the
    // Overview tab re-subscribes the contact, and a teammate's edit arrives
    // through the audit spine. Rebase every field the user has not touched
    // onto the new server value, so a change the user made elsewhere in this
    // panel does not read back as an unsaved edit and pop "Discard unsaved
    // changes?" on the way out (issue #415).
    const serverRef = React.useRef(contact);
    React.useEffect(() => {
        const prev = serverRef.current;
        if (prev === contact) return;
        serverRef.current = contact;
        setFirstName((v) => rebase(v, prev.first_name, contact.first_name));
        setLastName((v) => rebase(v, prev.last_name, contact.last_name));
        setEmail((v) => rebase(v, prev.email, contact.email));
        setCompany((v) => rebase(v, prev.company, contact.company));
        setPhone((v) => rebase(v, prev.phone, contact.phone));
        setSubscribed((v) => rebase(v, prev.subscribed, contact.subscribed));
        setCampaigns((v) => rebase(v, prev.campaigns ?? [], contact.campaigns ?? [], sameCampaigns));
        setCategoryIds((v) =>
            rebase(v, idsOf(prev.categories ?? []), idsOf(contact.categories ?? []), sameIDs),
        );
        // sameRows, not sameFields: a row the user has typed a value into but
        // not yet named saves as nothing, so the save-shaped comparison would
        // call the draft untouched and throw that row away.
        setCustomFields((v) =>
            rebase(v, fieldsOf(prev.custom_fields), fieldsOf(contact.custom_fields), sameRows),
        );
    }, [contact]);

    // What the save would send. The panel also treats a half-typed custom
    // field as unsaved work (see `dirty`), which this deliberately does not:
    // there is nothing to send for a row with no name.
    const changed = React.useMemo(() => {
        if (firstName !== contact.first_name) return true;
        if (lastName !== contact.last_name) return true;
        if (email !== contact.email) return true;
        if (company !== contact.company) return true;
        if (phone !== contact.phone) return true;
        if (subscribed !== contact.subscribed) return true;
        if (!sameFields(customFields, fieldsOf(contact.custom_fields))) return true;
        if (!sameCampaigns(campaigns, contact.campaigns ?? [])) return true;
        if (!sameIDs(categoryIds, idsOf(contact.categories ?? []))) return true;
        return false;
    }, [contact, firstName, lastName, email, company, phone, subscribed, customFields, campaigns, categoryIds]);

    // What the user would lose on the way out, which is more than what would
    // be sent: a custom-field row they have typed a value into but not named
    // yet is not savable and not, on its own, a reason to enable Save.
    const dirty = changed || hasUnnamedValue(customFields);

    async function save() {
        if (!changed) return;
        const data: Record<string, unknown> = {};
        if (firstName !== contact.first_name) data.first_name = firstName;
        if (lastName !== contact.last_name) data.last_name = lastName;
        if (email !== contact.email) data.email = email;
        if (company !== contact.company) data.company = company;
        if (phone !== contact.phone) data.phone = phone;
        if (subscribed !== contact.subscribed) data.subscribed = subscribed;
        // Same comparisons `dirty` and the rebase use, so what counts as
        // changed is decided in exactly one place.
        if (!sameFields(customFields, fieldsOf(contact.custom_fields))) {
            data.custom_fields = recordFromCF(customFields);
        }
        if (!sameCampaigns(campaigns, contact.campaigns ?? [])) data.campaigns = idsOf(campaigns);
        if (!sameIDs(categoryIds, idsOf(contact.categories ?? []))) data.categories = categoryIds;

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

    // Close, guarding unsaved edits behind the in-app confirm (never window.confirm).
    const requestClose = React.useCallback(() => {
        if (dirty) confirm.show("Discard unsaved changes?", onClose);
        else onClose();
    }, [dirty, onClose, confirm]);

    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key !== "Escape") return;
            // Innermost layer only: an open popover, confirm or dialog takes its own Escape.
            if (document.querySelector("[data-floating], [role='alertdialog'], [aria-modal='true']")) return;
            requestClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [requestClose]);

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
            onMouseDown={requestClose}
        >
            <motion.aside
                key="panel"
                initial={{ x: 32, opacity: 0 }}
                animate={{ x: 0, opacity: 1 }}
                exit={{ x: 32, opacity: 0 }}
                transition={{ duration: 0.2, ease: [0.32, 0.72, 0, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="flex flex-col w-full max-w-full md:w-[32rem] md:max-w-[95%] h-full bg-white border-l border-slate-200 shadow-[-12px_0_24px_-12px_rgba(15,23,42,0.08)]"
            >
                <ContactHeader
                    contact={contact}
                    displayName={displayName}
                    subscribed={subscribed}
                    suppressed={suppressed}
                    dirty={dirty && tab === "details"}
                    onClose={requestClose}
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
                    {tab === "activity" && <ActivityTab contactId={contact.id} contactName={firstName || lastName ? displayName : contact.email} />}
                    {tab === "notes" && <NotesTab contactId={contact.id} />}
                    {tab === "research" && <ResearchTab contactId={contact.id} />}
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
                            disabled={!changed || update.isPending}
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
    const [meetingOpen, setMeetingOpen] = React.useState(false);
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
        <>
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
                    <ResourceViewers resource={`contact:${contact.id}`} className="shrink-0" />
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
                onClick={() => useComposeStore.getState().openCompose(contact.email)}
                className="shrink-0 h-7 px-2 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors text-[12px]"
                title="Compose an email to this contact"
                aria-label="Compose email"
            >
                <MailIcon className="w-3.5 h-3.5" />
                <span className="hidden md:inline">Email</span>
            </button>
            <button
                type="button"
                onClick={() => setMeetingOpen(true)}
                className="shrink-0 h-7 px-2 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center gap-1.5 transition-colors text-[12px]"
                title="Schedule a call with this contact"
                aria-label="Schedule call"
            >
                <CalendarPlusIcon className="w-3.5 h-3.5" />
                <span className="hidden md:inline">Schedule call</span>
            </button>
            <BookACallButton
                email={contact.email}
                name={displayName}
                contactId={contact.id}
                className="shrink-0"
            />
            <button
                type="button"
                onClick={onClose}
                aria-label="Close"
                className="size-7 rounded-md text-slate-400 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors shrink-0"
            >
                <XIcon className="w-3.5 h-3.5" />
            </button>
            </header>
            <NewMeetingDialog
                open={meetingOpen}
                onClose={() => setMeetingOpen(false)}
                prefill={{
                    title: displayName ? `Call with ${displayName}` : "Call",
                    name: displayName,
                    email: contact.email,
                    contactId: contact.id,
                }}
            />
        </>
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
        <nav className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200 overflow-x-auto md:overflow-visible">
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
