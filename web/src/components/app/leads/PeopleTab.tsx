import { useState } from "react";
import toast from "react-hot-toast/headless";
import {
    CopyIcon,
    ExternalLinkIcon,
    Loader2Icon,
    MoreHorizontalIcon,
    PencilIcon,
    RefreshCwIcon,
    SearchIcon,
    Trash2Icon,
    UserPlusIcon,
    UsersIcon,
} from "lucide-react";
import { TextInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuItem, PopoverMenuSeparator, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import { useAddLeadPerson, useDeleteLeadPerson, useFindLeadPersonEmail, useUpdateLeadPerson } from "@/lib/api/hooks/app/leads/useLeads";
import type { Lead, LeadPerson, LeadPersonSource, LeadSeniority } from "@/lib/api/models/app/leads/Lead";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";
import { personVerdict } from "./verdict";

const SENIORITY: Record<LeadSeniority, { label: string; tone: string; avatar: string }> = {
    owner: { label: "Owner", tone: "bg-violet-50 text-violet-700", avatar: "bg-violet-100 text-violet-700" },
    executive: { label: "Executive", tone: "bg-sky-50 text-sky-700", avatar: "bg-sky-100 text-sky-700" },
    manager: { label: "Manager", tone: "bg-teal-50 text-teal-700", avatar: "bg-teal-100 text-teal-700" },
    staff: { label: "", tone: "", avatar: "bg-slate-100 text-slate-600" },
};

const SOURCE: Record<LeadPersonSource, string> = {
    schema: "Listed on the website",
    page: "From the website",
    imprint: "From the legal notice",
    email: "From an address on the website",
    linkedin: "Linked from the website",
    manual: "Added by your team",
};

function initials(p: LeadPerson) {
    const a = (p.first_name || p.name).charAt(0);
    const b = p.last_name.charAt(0);
    return (a + b).toUpperCase();
}

function copy(v: string) {
    navigator.clipboard?.writeText(v).then(
        () => toast.success("Copied"),
        () => toast.error("Could not copy"),
    );
}

export default function PeopleTab({ lead, onFindPeople, finding }: { lead: Lead; onFindPeople: () => void; finding: boolean }) {
    const people = lead.people ?? [];
    const pattern = lead.site.email_pattern;
    const domain = lead.domain;
    return (
        <>
            {people.length === 0 ? (
                <div className="rounded-lg border border-dashed border-slate-200 px-4 py-5 text-center">
                    <div className="mx-auto mb-2 size-8 rounded-full bg-slate-50 text-slate-400 flex items-center justify-center">
                        <UsersIcon className="size-4" />
                    </div>
                    <p className="text-[12.5px] font-medium text-slate-800">
                        {lead.enrichment_status === "done" ? "The website names nobody who works here" : "Find the people who run this business"}
                    </p>
                    <p className="mx-auto mt-1 max-w-[280px] text-[11.5px] leading-relaxed text-slate-500">
                        {lead.enrichment_status === "done"
                            ? "Add someone you know of below and their address is worked out from the domain."
                            : "Reading the website also goes through its team, about and legal pages for owners, managers and staff, then works out and checks their addresses."}
                    </p>
                    {lead.website && lead.enrichment_status !== "done" && (
                        <button
                            type="button"
                            onClick={onFindPeople}
                            disabled={finding}
                            className="mt-3 h-7 px-2.5 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                        >
                            {finding ? <Loader2Icon className="size-3.5 animate-spin" /> : <SearchIcon className="size-3.5" />}
                            {finding ? "Reading website…" : "Find people"}
                        </button>
                    )}
                </div>
            ) : (
                <>
                    {(pattern || lead.site.accepts_all) && domain && (
                        <div className="mb-3 rounded-md border border-slate-200 bg-slate-50/60 px-2.5 py-2 text-[11.5px] leading-relaxed text-slate-600">
                            {pattern && (
                                <p>
                                    Addresses here look like <span className="font-mono text-[11px] text-slate-800">{patternExample(pattern)}@{domain}</span>
                                </p>
                            )}
                            {lead.site.accepts_all && (
                                <p className={cn(pattern && "mt-0.5")}>Their mail server accepts any address, so worked-out ones cannot be confirmed.</p>
                            )}
                        </div>
                    )}
                    <ul className="space-y-1.5">
                        {people.map((p) => (
                            <PersonRow key={p.id} lead={lead} person={p} />
                        ))}
                    </ul>
                </>
            )}
            <AddPerson lead={lead} />
        </>
    );
}

const PATTERN_EXAMPLE: Record<string, string> = {
    "first.last": "jane.doe",
    first: "jane",
    flast: "jdoe",
    firstlast: "janedoe",
    "f.last": "j.doe",
    last: "doe",
    first_last: "jane_doe",
    "first-last": "jane-doe",
    "last.first": "doe.jane",
    lastf: "doej",
    firstl: "janed",
    "first.l": "jane.d",
};

function patternExample(pattern: string) {
    return PATTERN_EXAMPLE[pattern] ?? pattern;
}

function PersonRow({ lead, person: p }: { lead: Lead; person: LeadPerson }) {
    const confirm = useConfirm();
    const find = useFindLeadPersonEmail();
    const remove = useDeleteLeadPerson();
    const [editing, setEditing] = useState(false);
    const seniority = SENIORITY[p.seniority];
    const v = personVerdict(p);
    const usable = p.email && p.email_status !== "suppressed" && p.email_status !== "invalid";

    const findAgain = async () => {
        try {
            const out = await find.mutateAsync({ leadId: lead.id, personId: p.id });
            toast.success(out.email ? `${out.email} · ${personVerdict(out).label}` : `No address found for ${out.name}`);
        } catch (e) {
            toast.error(buildError(e as unknown as AppError));
        }
    };

    if (editing) return <PersonEditor lead={lead} person={p} onDone={() => setEditing(false)} />;

    return (
        <li className="group rounded-md border border-slate-200 px-2.5 py-2">
            <div className="flex items-start gap-2.5">
                <div className={cn("mt-0.5 size-7 shrink-0 rounded-full text-[11px] font-semibold flex items-center justify-center", seniority.avatar)}>{initials(p)}</div>
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                        <span className="truncate text-[12.5px] font-medium text-slate-900">{p.name}</span>
                        {seniority.label && <span className={cn("shrink-0 rounded px-1 text-[10px]", seniority.tone)}>{seniority.label}</span>}
                        {p.contact_id && <span className="shrink-0 rounded bg-emerald-50 px-1 text-[10px] text-emerald-700">In contacts</span>}
                    </div>
                    {p.title && <p className="truncate text-[11.5px] text-slate-500">{p.title}</p>}
                    {p.email ? (
                        <div className="mt-1 flex items-center gap-1.5">
                            <span className={cn("truncate text-[12px]", usable ? "text-slate-800" : "text-slate-400 line-through")}>{p.email}</span>
                            <button
                                type="button"
                                aria-label={`Copy ${p.email}`}
                                onClick={() => copy(p.email)}
                                className="size-5 shrink-0 rounded text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100"
                            >
                                <CopyIcon className="size-3" />
                            </button>
                        </div>
                    ) : (
                        <p className="mt-1 text-[11.5px] text-slate-400">{find.isPending ? "Working out their address…" : "No address yet"}</p>
                    )}
                    <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-[10.5px] text-slate-400">
                        {p.email && (
                            <>
                                <span className={cn("inline-flex items-center gap-0.5", v.tone)} title={v.hint}>
                                    {v.icon}
                                    {v.label}
                                    {p.email_confidence > 0 && p.email_status !== "suppressed" && <span className="text-slate-400"> · {p.email_confidence}%</span>}
                                </span>
                                <span>·</span>
                            </>
                        )}
                        {p.source_url ? (
                            <a href={p.source_url} target="_blank" rel="noopener noreferrer nofollow" className="hover:text-slate-700">
                                {SOURCE[p.source]}
                            </a>
                        ) : (
                            <span>{SOURCE[p.source]}</span>
                        )}
                    </div>
                </div>
                <div className="flex shrink-0 items-center gap-0.5">
                    {p.linkedin_url && (
                        <a
                            href={p.linkedin_url}
                            target="_blank"
                            rel="noopener noreferrer nofollow"
                            aria-label={`${p.name} on LinkedIn`}
                            className="h-6 px-1.5 rounded text-[10.5px] font-semibold text-[#0A66C2] hover:bg-sky-50 inline-flex items-center gap-0.5"
                        >
                            in
                            <ExternalLinkIcon className="size-2.5" />
                        </a>
                    )}
                    <PopoverMenu align="end">
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                aria-label={`More for ${p.name}`}
                                className="size-6 rounded text-slate-400 hover:text-slate-800 hover:bg-slate-100 inline-flex items-center justify-center"
                            >
                                {find.isPending ? <Loader2Icon className="size-3.5 animate-spin" /> : <MoreHorizontalIcon className="size-3.5" />}
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={190}>
                            <PopoverMenuItem icon={<RefreshCwIcon className="size-3.5" />} onSelect={findAgain} disabled={find.isPending || !lead.domain}>
                                {p.email_source === "pattern" || !p.email ? "Find their email again" : "Check the address again"}
                            </PopoverMenuItem>
                            <PopoverMenuItem icon={<PencilIcon className="size-3.5" />} onSelect={() => setEditing(true)}>
                                Edit
                            </PopoverMenuItem>
                            <PopoverMenuSeparator />
                            <PopoverMenuItem
                                danger
                                icon={<Trash2Icon className="size-3.5" />}
                                onSelect={() =>
                                    confirm.show(`Remove ${p.name} from ${lead.name}? A contact made from them stays.`, async () => {
                                        try {
                                            await remove.mutateAsync({ leadId: lead.id, personId: p.id });
                                        } catch (e) {
                                            toast.error(buildError(e as unknown as AppError));
                                        }
                                    })
                                }
                            >
                                Remove
                            </PopoverMenuItem>
                        </PopoverMenuContent>
                    </PopoverMenu>
                </div>
            </div>
        </li>
    );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <label className="block min-w-0">
            <span className="mb-0.5 block text-[10.5px] text-slate-500">{label}</span>
            {children}
        </label>
    );
}

function PersonEditor({ lead, person, onDone }: { lead: Lead; person: LeadPerson; onDone: () => void }) {
    const update = useUpdateLeadPerson();
    const [first, setFirst] = useState(person.first_name);
    const [last, setLast] = useState(person.last_name);
    const [title, setTitle] = useState(person.title);
    const [email, setEmail] = useState(person.email);
    const [linkedin, setLinkedin] = useState(person.linkedin_url);
    const save = async () => {
        const patch: Record<string, string> = {};
        if (first !== person.first_name) patch.first_name = first;
        if (last !== person.last_name) patch.last_name = last;
        if (title !== person.title) patch.title = title;
        if (email.trim() !== person.email) patch.email = email;
        if (linkedin !== person.linkedin_url) patch.linkedin_url = linkedin;
        if (Object.keys(patch).length === 0) return onDone();
        try {
            await update.mutateAsync({ leadId: lead.id, personId: person.id, patch });
            onDone();
        } catch (e) {
            toast.error(buildError(e as unknown as AppError));
        }
    };
    return (
        <li className="rounded-md border border-sky-200 bg-sky-50/30 px-2.5 py-2.5">
            <div className="grid grid-cols-2 gap-2">
                <Field label="First name">
                    <TextInput value={first} onChange={setFirst} maxLength={60} autoFocus />
                </Field>
                <Field label="Last name">
                    <TextInput value={last} onChange={setLast} maxLength={60} />
                </Field>
                <div className="col-span-2">
                    <Field label="Title">
                        <TextInput value={title} onChange={setTitle} maxLength={120} placeholder="Owner, office manager…" />
                    </Field>
                </div>
                <div className="col-span-2">
                    <Field label="Email">
                        <TextInput value={email} onChange={setEmail} type="email" maxLength={254} placeholder="Leave empty to work it out" />
                    </Field>
                </div>
                <div className="col-span-2">
                    <Field label="LinkedIn">
                        <TextInput value={linkedin} onChange={setLinkedin} maxLength={300} placeholder="https://www.linkedin.com/in/…" />
                    </Field>
                </div>
            </div>
            <div className="mt-2.5 flex items-center justify-end gap-1.5">
                <button type="button" onClick={onDone} className="h-7 px-2.5 rounded-md text-[12px] text-slate-600 hover:text-slate-900">
                    Cancel
                </button>
                <button
                    type="button"
                    onClick={save}
                    disabled={update.isPending || !first.trim()}
                    className="h-7 px-2.5 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                >
                    {update.isPending && <Loader2Icon className="size-3.5 animate-spin" />}
                    Save
                </button>
            </div>
        </li>
    );
}

function AddPerson({ lead }: { lead: Lead }) {
    const add = useAddLeadPerson();
    const [first, setFirst] = useState("");
    const [last, setLast] = useState("");
    const [title, setTitle] = useState("");
    const [email, setEmail] = useState("");
    const ready = first.trim() !== "" && (last.trim() !== "" || email.trim() !== "") && (!!lead.domain || email.trim() !== "");
    const submit = async () => {
        if (!ready || add.isPending) return;
        try {
            const p = await add.mutateAsync({ leadId: lead.id, input: { first_name: first, last_name: last, title, email } });
            setFirst("");
            setLast("");
            setTitle("");
            setEmail("");
            toast.success(p.email ? `${p.email} · ${personVerdict(p).label}` : `Added ${p.name}; no address could be worked out`);
        } catch (e) {
            toast.error(buildError(e as unknown as AppError));
        }
    };
    const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
        if (e.key === "Enter") {
            e.preventDefault();
            submit();
        }
    };
    return (
        <section className="mt-4">
            <h3 className="mb-1 text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Find someone's email</h3>
            <p className="mb-2 text-[11.5px] leading-relaxed text-slate-500">
                {lead.domain
                    ? `Know who you want? Their address at ${lead.domain} is worked out from their name and checked with the mail server.`
                    : "This business lists no website, so there is no domain to work an address out at. You can still add someone whose address you know."}
            </p>
            <div className="grid grid-cols-2 gap-2">
                <Field label="First name">
                    <TextInput value={first} onChange={setFirst} maxLength={60} placeholder="Jane" onKeyDown={onKeyDown} />
                </Field>
                <Field label="Last name">
                    <TextInput value={last} onChange={setLast} maxLength={60} placeholder="Doe" onKeyDown={onKeyDown} />
                </Field>
                <Field label="Title (optional)">
                    <TextInput value={title} onChange={setTitle} maxLength={120} placeholder="Owner" onKeyDown={onKeyDown} />
                </Field>
                <Field label={lead.domain ? "Email, if you know it" : "Email"}>
                    <TextInput value={email} onChange={setEmail} type="email" maxLength={254} placeholder={lead.domain ? "Worked out if empty" : "jane@…"} onKeyDown={onKeyDown} />
                </Field>
            </div>
            <div className="mt-2 flex items-center justify-between gap-2">
                <p className="text-[11px] text-slate-400">{!ready && first.trim() ? (lead.domain ? "Add a last name to work out the address" : "Add their email address") : ""}</p>
                <button
                    type="button"
                    onClick={submit}
                    disabled={!ready || add.isPending}
                    className="h-7 shrink-0 px-2.5 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-50"
                >
                    {add.isPending ? <Loader2Icon className="size-3.5 animate-spin" /> : email.trim() || !lead.domain ? <UserPlusIcon className="size-3.5" /> : <SearchIcon className="size-3.5" />}
                    {add.isPending ? "Checking…" : email.trim() || !lead.domain ? "Add person" : "Find email"}
                </button>
            </div>
        </section>
    );
}
