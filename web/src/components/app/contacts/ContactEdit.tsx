// Edit contact slide-over — brae density.
//
// Replaces the old blue-button-heavy panel. Visual rules:
//   - 32rem wide right-side panel
//   - slate-900 primary action, hairline borders, 12.5px body text
//   - single scrollable column with grouped sections
//   - sticky footer with Discard / Save
//   - dirty-tracking surfaces an unsaved-changes pill in the header
//
// Reuses the shared <TextInput> + <Label> primitives so the look
// matches NewContactDialog exactly.

import React from "react";
import { AnimatePresence, motion } from "framer-motion";
import {
    BuildingIcon,
    CalendarIcon,
    CheckIcon,
    Loader2Icon,
    MailIcon,
    PhoneIcon,
    PlusIcon,
    SearchIcon,
    SendIcon,
    TrashIcon,
    UserIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import { Label, TextInput } from "@/components/ui/field";
import useUpdateContact from "@/lib/api/hooks/app/contacts/useUpdateContact";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useClickOutside from "@/hooks/useClickOutside";

interface CustomField {
    name: string;
    value: string;
}

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

    const [firstName, setFirstName] = React.useState(contact.first_name);
    const [lastName, setLastName] = React.useState(contact.last_name);
    const [email, setEmail] = React.useState(contact.email);
    const [company, setCompany] = React.useState(contact.company);
    const [phone, setPhone] = React.useState(contact.phone);
    const [subscribed, setSubscribed] = React.useState(contact.subscribed);
    const [campaigns, setCampaigns] = React.useState<MiniCampaign[]>(contact.campaigns ?? []);
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
        const cur = new Set(contact.campaigns.map((c) => c.id));
        const next = new Set(campaigns.map((c) => c.id));
        if (cur.size !== next.size) return true;
        for (const id of cur) if (!next.has(id)) return true;
        return false;
    }, [contact, firstName, lastName, email, company, phone, subscribed, customFields, campaigns, recordFromCF]);

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

    // Esc to close, with a confirm if dirty.
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
                {/* Header */}
                <header className="h-12 px-4 border-b border-slate-200 flex items-center gap-2.5 shrink-0">
                    <div className="size-7 rounded-md bg-slate-900 text-white flex items-center justify-center text-[11px] font-semibold shrink-0">
                        {(contact.email || "?").slice(0, 2).toUpperCase()}
                    </div>
                    <div className="min-w-0 flex-1">
                        <div className="text-[12.5px] text-slate-900 font-medium truncate leading-tight">
                            {firstName || lastName
                                ? `${firstName} ${lastName}`.trim()
                                : "Edit contact"}
                        </div>
                        <div className="text-[10.5px] text-slate-500 truncate font-mono leading-tight">
                            {contact.email}
                        </div>
                    </div>
                    {dirty && (
                        <span className="text-[10px] uppercase tracking-[0.14em] text-amber-700 bg-amber-50 px-1.5 py-0.5 rounded-sm font-medium border border-amber-100">
                            Unsaved
                        </span>
                    )}
                    <button
                        type="button"
                        onClick={() => (dirty && !window.confirm("Discard unsaved changes?") ? undefined : onClose())}
                        aria-label="Close"
                        className="size-7 rounded-md text-slate-500 hover:text-slate-900 hover:bg-slate-100 inline-flex items-center justify-center transition-colors"
                    >
                        <XIcon className="w-3.5 h-3.5" />
                    </button>
                </header>

                {/* Body */}
                <div className="flex-1 min-h-0 overflow-y-auto px-5 py-5 space-y-6">
                    <Section title="Identity">
                        <div className="grid grid-cols-2 gap-2">
                            <Field label="First name" icon={<UserIcon className="w-3 h-3" />}>
                                <TextInput value={firstName} onChange={setFirstName} className="w-full" placeholder="John" />
                            </Field>
                            <Field label="Last name">
                                <TextInput value={lastName} onChange={setLastName} className="w-full" placeholder="Doe" />
                            </Field>
                        </div>
                        <Field label="Email" icon={<MailIcon className="w-3 h-3" />}>
                            <TextInput value={email} onChange={setEmail} className="w-full" placeholder="name@company.com" type="email" />
                        </Field>
                        <div className="grid grid-cols-2 gap-2">
                            <Field label="Company" icon={<BuildingIcon className="w-3 h-3" />}>
                                <TextInput value={company} onChange={setCompany} className="w-full" placeholder="Acme Inc." />
                            </Field>
                            <Field label="Phone" icon={<PhoneIcon className="w-3 h-3" />}>
                                <TextInput value={phone} onChange={setPhone} className="w-full" placeholder="+1 555…" />
                            </Field>
                        </div>
                    </Section>

                    <Section title="Subscription">
                        <ToggleRow
                            label="Subscribed"
                            description="Unsubscribed contacts are skipped by every campaign."
                            on={subscribed}
                            onChange={setSubscribed}
                        />
                    </Section>

                    <Section
                        title="Campaigns"
                        accessory={
                            <span className="text-[10.5px] text-slate-400 tabular-nums">
                                {campaigns.length} active
                            </span>
                        }
                    >
                        <CampaignPicker selected={campaigns} setSelected={setCampaigns} />
                    </Section>

                    <Section
                        title="Custom fields"
                        accessory={
                            <button
                                type="button"
                                onClick={() => setCustomFields((f) => [...f, { name: "", value: "" }])}
                                className="h-6 px-2 rounded-md border border-slate-200 hover:border-slate-300 text-[11px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1 transition-colors"
                            >
                                <PlusIcon className="w-3 h-3" />
                                Add field
                            </button>
                        }
                    >
                        {customFields.length === 0 ? (
                            <div className="rounded-md border border-dashed border-slate-200 px-3 py-4 text-[11.5px] text-slate-400 text-center">
                                No custom fields. Add one to attach extra metadata.
                            </div>
                        ) : (
                            <div className="space-y-1.5">
                                {customFields.map((f, idx) => (
                                    <div key={idx} className="flex items-start gap-1.5">
                                        <TextInput
                                            value={f.name}
                                            onChange={(v) =>
                                                setCustomFields((cur) =>
                                                    cur.map((c, i) => (i === idx ? { ...c, name: v } : c)),
                                                )
                                            }
                                            placeholder="key"
                                            className="w-[140px]"
                                        />
                                        <TextInput
                                            value={f.value}
                                            onChange={(v) =>
                                                setCustomFields((cur) =>
                                                    cur.map((c, i) => (i === idx ? { ...c, value: v } : c)),
                                                )
                                            }
                                            placeholder="value"
                                            className="flex-1"
                                        />
                                        <button
                                            type="button"
                                            onClick={() =>
                                                setCustomFields((cur) => cur.filter((_, i) => i !== idx))
                                            }
                                            aria-label="Remove field"
                                            className="size-7 rounded-md text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center transition-colors shrink-0"
                                        >
                                            <TrashIcon className="w-3 h-3" />
                                        </button>
                                    </div>
                                ))}
                            </div>
                        )}
                    </Section>

                    <Section title="Activity">
                        <div className="grid grid-cols-2 gap-3 text-[11.5px]">
                            <MetaRow
                                icon={<CalendarIcon className="w-3 h-3 text-slate-400" />}
                                label="Created"
                                value={fmt(contact.created_at)}
                            />
                            <MetaRow
                                icon={<CalendarIcon className="w-3 h-3 text-slate-400" />}
                                label="Updated"
                                value={fmt(contact.updated_at)}
                            />
                        </div>
                    </Section>
                </div>

                {/* Footer */}
                <footer className="h-12 px-3 border-t border-slate-200 flex items-center gap-1.5 shrink-0 bg-slate-50/30">
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
            </motion.aside>
        </motion.div>
    );
}

function Section({
    title,
    accessory,
    children,
}: {
    title: string;
    accessory?: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <section>
            <div className="flex items-center gap-2 mb-2">
                <h2 className="text-[10px] uppercase tracking-[0.14em] font-semibold text-slate-500">
                    {title}
                </h2>
                <div className="flex-1 h-px bg-slate-200" />
                {accessory}
            </div>
            <div className="space-y-2">{children}</div>
        </section>
    );
}

function Field({
    label,
    icon,
    children,
}: {
    label: string;
    icon?: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <div>
            <Label className="flex items-center gap-1 text-slate-500">
                {icon}
                {label}
            </Label>
            {children}
        </div>
    );
}

function ToggleRow({
    label,
    description,
    on,
    onChange,
}: {
    label: string;
    description: string;
    on: boolean;
    onChange: (v: boolean) => void;
}) {
    return (
        <div className="flex items-center gap-3 rounded-md border border-slate-200 bg-white px-3 py-2.5">
            <div className="min-w-0 flex-1">
                <div className="text-[12.5px] text-slate-900 font-medium leading-tight">{label}</div>
                <div className="text-[11px] text-slate-500 leading-tight mt-0.5">{description}</div>
            </div>
            <button
                type="button"
                onClick={() => onChange(!on)}
                role="switch"
                aria-checked={on}
                className={`relative h-4 w-7 rounded-full transition-colors shrink-0 ${
                    on ? "bg-slate-900" : "bg-slate-200"
                }`}
            >
                <span
                    className={`absolute top-0.5 left-0.5 size-3 rounded-full bg-white transition-transform ${
                        on ? "translate-x-3" : "translate-x-0"
                    }`}
                />
            </button>
        </div>
    );
}

function MetaRow({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
    return (
        <div className="rounded-md border border-slate-200 bg-white px-3 py-2">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium flex items-center gap-1">
                {icon}
                {label}
            </div>
            <div className="text-[12px] text-slate-900 font-mono tabular-nums mt-0.5">{value}</div>
        </div>
    );
}

function fmt(d: Date | string) {
    try {
        const dt = typeof d === "string" ? new Date(d) : d;
        return dt.toLocaleString("en-US", {
            month: "short",
            day: "numeric",
            year: "numeric",
            hour: "2-digit",
            minute: "2-digit",
        });
    } catch {
        return "—";
    }
}

/**
 * Inline campaign picker — pill list on top, searchable dropdown
 * below. Slate-themed, no popup library.
 */
function CampaignPicker({
    selected,
    setSelected,
}: {
    selected: MiniCampaign[];
    setSelected: React.Dispatch<React.SetStateAction<MiniCampaign[]>>;
}) {
    const [open, setOpen] = React.useState(false);
    const [search, setSearch] = React.useState("");
    const [enabled, setEnabled] = React.useState(false);
    const ref = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));

    React.useEffect(() => {
        if (open) setEnabled(true);
    }, [open]);

    const { campaigns } = useCampaigns({ query: search, folder: "", enabled });

    function toggle(c: MiniCampaign | { id: string; name: string }) {
        setSelected((cur) => {
            if (cur.some((x) => x.id === c.id)) return cur.filter((x) => x.id !== c.id);
            return [...cur, { id: c.id, name: c.name }];
        });
    }

    return (
        <div ref={ref} className="relative">
            <div className="rounded-md border border-slate-200 bg-white">
                {selected.length === 0 ? (
                    <div
                        onClick={() => setOpen((o) => !o)}
                        className="px-3 py-2 text-[11.5px] text-slate-400 cursor-pointer hover:text-slate-600"
                    >
                        No campaigns selected. Click to add.
                    </div>
                ) : (
                    <div className="px-2 py-2 flex flex-wrap gap-1">
                        {selected.map((c) => (
                            <span
                                key={c.id}
                                className="inline-flex items-center gap-1 h-5 pl-1.5 pr-1 rounded text-[11px] font-medium bg-slate-900 text-white"
                            >
                                <SendIcon className="w-2.5 h-2.5" />
                                {c.name}
                                <button
                                    type="button"
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        toggle(c);
                                    }}
                                    className="opacity-70 hover:opacity-100"
                                    aria-label={`Remove ${c.name}`}
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
                        ))}
                        <button
                            type="button"
                            onClick={() => setOpen((o) => !o)}
                            className="inline-flex items-center gap-1 h-5 px-1.5 rounded text-[11px] font-medium border border-dashed border-slate-300 text-slate-500 hover:border-slate-400 hover:text-slate-700"
                        >
                            <PlusIcon className="w-2.5 h-2.5" />
                            Add
                        </button>
                    </div>
                )}
            </div>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -4 }}
                        transition={{ duration: 0.12 }}
                        className="absolute top-full left-0 right-0 mt-1 z-20 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden"
                    >
                        <div className="px-2 py-1.5 border-b border-slate-200 flex items-center gap-1.5">
                            <SearchIcon className="w-3 h-3 text-slate-400" />
                            <input
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                placeholder="Search campaigns…"
                                autoFocus
                                className="flex-1 h-5 bg-transparent text-[12px] text-slate-900 placeholder:text-slate-400 outline-none"
                            />
                        </div>
                        <div className="max-h-56 overflow-y-auto py-1">
                            {campaigns.length === 0 ? (
                                <div className="px-3 py-3 text-[11.5px] text-slate-400 text-center">
                                    No campaigns found.
                                </div>
                            ) : (
                                campaigns.map((c) => {
                                    const checked = selected.some((s) => s.id === c.id);
                                    return (
                                        <button
                                            key={c.id}
                                            type="button"
                                            onClick={() => toggle(c)}
                                            className="w-full px-2.5 h-7 flex items-center gap-2 text-[12px] text-slate-700 hover:bg-slate-100 transition-colors"
                                        >
                                            <span
                                                className={`size-3.5 rounded border flex items-center justify-center transition-colors ${
                                                    checked
                                                        ? "border-slate-900 bg-slate-900"
                                                        : "border-slate-300 bg-white"
                                                }`}
                                            >
                                                {checked && <CheckIcon className="w-2 h-2 text-white" />}
                                            </span>
                                            <SendIcon className="w-3 h-3 text-slate-400" />
                                            <span className="truncate">{c.name}</span>
                                        </button>
                                    );
                                })
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
