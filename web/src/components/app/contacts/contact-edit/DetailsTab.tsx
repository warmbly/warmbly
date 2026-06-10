// Details tab — the original "edit everything" form, lifted out of
// ContactEdit.tsx now that the slide-over has multiple tabs.
//
// All the form state still lives in the parent (so the dirty
// indicator + sticky footer can stay attached to the wrapper); this
// component only renders fields.

import React from "react";
import {
    BuildingIcon,
    CalendarIcon,
    CheckIcon,
    MailIcon,
    PhoneIcon,
    PlusIcon,
    SearchIcon,
    SendIcon,
    TrashIcon,
    UserIcon,
    XIcon,
} from "lucide-react";
import { AnimatePresence, motion } from "framer-motion";
import { Label, TextInput } from "@/components/ui/field";
import useCampaigns from "@/lib/api/hooks/app/campaigns/useCampaigns";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import type MiniCampaign from "@/lib/api/models/app/campaigns/MiniCampaign";
import useClickOutside from "@/hooks/useClickOutside";
import useFlipPlacement from "@/hooks/useFlipPlacement";
import CategoryPicker from "../CategoryPicker";
import { fmtAbsolute } from "./format";

export interface CustomField {
    name: string;
    value: string;
}

export default function DetailsTab({
    contact,
    firstName,
    setFirstName,
    lastName,
    setLastName,
    email,
    setEmail,
    company,
    setCompany,
    phone,
    setPhone,
    subscribed,
    setSubscribed,
    campaigns,
    setCampaigns,
    categoryIds,
    setCategoryIds,
    customFields,
    setCustomFields,
}: {
    contact: Contact;
    firstName: string;
    setFirstName: (v: string) => void;
    lastName: string;
    setLastName: (v: string) => void;
    email: string;
    setEmail: (v: string) => void;
    company: string;
    setCompany: (v: string) => void;
    phone: string;
    setPhone: (v: string) => void;
    subscribed: boolean;
    setSubscribed: (v: boolean) => void;
    campaigns: MiniCampaign[];
    setCampaigns: React.Dispatch<React.SetStateAction<MiniCampaign[]>>;
    categoryIds: string[];
    setCategoryIds: (v: string[]) => void;
    customFields: CustomField[];
    setCustomFields: React.Dispatch<React.SetStateAction<CustomField[]>>;
}) {
    return (
        <div className="space-y-6">
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
                title="Categories"
                accessory={
                    <span className="text-[10.5px] text-slate-400 tabular-nums">
                        {categoryIds.length}
                    </span>
                }
            >
                <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
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
                                    className="w-[110px] md:w-[140px]"
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

            <Section title="Metadata">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-[11.5px]">
                    <MetaRow
                        icon={<CalendarIcon className="w-3 h-3 text-slate-400" />}
                        label="Created"
                        value={fmtAbsolute(contact.created_at)}
                    />
                    <MetaRow
                        icon={<CalendarIcon className="w-3 h-3 text-slate-400" />}
                        label="Updated"
                        value={fmtAbsolute(contact.updated_at)}
                    />
                </div>
            </Section>
        </div>
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

/**
 * Inline campaign picker — same component that previously lived
 * inside ContactEdit. Lifted here verbatim so the Details tab keeps
 * working identically.
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
    const triggerRef = React.useRef<HTMLDivElement>(null);
    useClickOutside(ref, () => setOpen(false));
    const placement = useFlipPlacement(triggerRef, open, 290);

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
            <div ref={triggerRef} className="rounded-md border border-slate-200 bg-white">
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
                        initial={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: placement === "top" ? 4 : -4 }}
                        transition={{ duration: 0.12 }}
                        className={`absolute left-0 right-0 z-20 rounded-md border border-slate-200 bg-white shadow-[0_12px_32px_-8px_rgba(15,23,42,0.18)] overflow-hidden ${
                            placement === "top" ? "bottom-full mb-1" : "top-full mt-1"
                        }`}
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
