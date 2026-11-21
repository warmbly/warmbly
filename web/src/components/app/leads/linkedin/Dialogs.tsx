// The finder's dialogs: add to contacts, paste a LinkedIn URL, save a search, and the saved searches menu.
import { Link } from "@tanstack/react-router";
import { ArrowRightIcon, BookmarkIcon, CheckCircle2Icon, CircleAlertIcon, ClipboardPasteIcon, FolderTreeIcon, Loader2Icon, MegaphoneIcon, PencilIcon, TagsIcon, Trash2Icon, UserPlusIcon, XIcon, type LucideIcon } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import toast from "react-hot-toast/headless";
import { Toggle } from "@/components/app/campaigns/preferences/components/CampaignPreferenceBoolBox";
import CategoryPicker from "@/components/app/contacts/CategoryPicker";
import LeadDialog from "@/components/app/leads/LeadDialog";
import { CampaignMultiPicker, SegmentMultiPicker } from "@/components/app/segments/SegmentPickers";
import { TextInput } from "@/components/ui/field";
import { PopoverMenu, PopoverMenuContent, PopoverMenuLabel, PopoverMenuTrigger } from "@/components/ui/popover-menu";
import { useConfirm } from "@/hooks/context/confirm";
import {
    importFinished,
    useCreateSavedSearch,
    useDeleteSavedSearch,
    useImportFinderHits,
    useLinkedInImport,
    useParseFinderUrl,
    usePatchSavedSearch,
    useSavedSearches,
} from "@/lib/api/hooks/app/social/useSocial";
import { useLeadFinderConfig } from "@/lib/api/hooks/app/leads/useLeads";
import type { FinderQuery, ImportResult, ParsedUrl, SavedSearch } from "@/lib/api/models/app/social/Social";
import { fmtUsdCost } from "@/lib/money";
import { socialErrorMessage } from "@/lib/social/errors";
import { cn } from "@/lib/utils";
import { SOURCE_LABELS } from "./state";

const newKey = () => (typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`);

export interface ImportTarget {
    accountId: string | null;
    query: FinderQuery;
    keys: string[];
    count: number;
}

export function ImportDialog({ open, onClose, target, suggestedSegment, onDone }: { open: boolean; onClose: () => void; target: ImportTarget; suggestedSegment?: string; onDone: () => void }) {
    const imp = useImportFinderHits();
    const [segmentIds, setSegmentIds] = useState<string[]>([]);
    const [categoryIds, setCategoryIds] = useState<string[]>([]);
    const [campaignIds, setCampaignIds] = useState<string[]>([]);
    const [skipExisting, setSkipExisting] = useState(false);
    const [findEmails, setFindEmails] = useState(false);
    const emailCost = useLeadFinderConfig().data?.person_email_cost_usd;
    const [result, setResult] = useState<ImportResult | null>(null);
    // One key per opening, so a retried click is the same import.
    const key = useRef(newKey());
    const attempt = useRef(0);
    useEffect(() => {
        if (!open) return;
        key.current = newKey();
        setResult(null);
    }, [open]);
    const close = () => {
        attempt.current++;
        onClose();
    };
    const submit = async () => {
        const mine = ++attempt.current;
        try {
            const res = await imp.mutateAsync({
                idempotencyKey: key.current,
                body: {
                    query: target.query,
                    keys: target.keys,
                    account_id: target.accountId,
                    segment_ids: segmentIds,
                    category_ids: categoryIds,
                    campaign_ids: campaignIds,
                    skip_existing: skipExisting,
                    find_emails: findEmails,
                },
            });
            if (mine !== attempt.current) return;
            if ("import_id" in res) {
                // Finding emails runs in the background; its progress lives in a toast, not this dialog.
                showImportProgress(res.import_id);
                onDone();
                close();
                return;
            }
            setResult(res);
            onDone();
            const n = res.created + res.updated;
            if (n > 0) toast.success(`${n.toLocaleString()} contact${n === 1 ? "" : "s"} added`);
        } catch (e) {
            toast.error(socialErrorMessage(e));
        }
    };
    return (
        <LeadDialog
            open={open}
            onClose={close}
            width={560}
            icon={<UserPlusIcon className="size-3" />}
            eyebrow="Add to contacts"
            title={`${target.count.toLocaleString()} ${target.count === 1 ? "person" : "people"} from LinkedIn`}
            footer={
                result ? (
                    <>
                        <Link to="/app/contacts" onClick={close} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900 inline-flex items-center gap-1">
                            Open contacts
                            <ArrowRightIcon className="size-3" />
                        </Link>
                        <button type="button" onClick={close} className="h-7 px-3 rounded-md bg-slate-900 text-white text-[12px] font-medium">
                            Done
                        </button>
                    </>
                ) : (
                    <>
                        <button type="button" onClick={close} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900">
                            Cancel
                        </button>
                        <button
                            type="button"
                            onClick={submit}
                            disabled={imp.isPending || target.count === 0}
                            className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                        >
                            {imp.isPending ? <Loader2Icon className="size-3.5 animate-spin" /> : <UserPlusIcon className="size-3.5" />}
                            Add to contacts
                        </button>
                    </>
                )
            }
        >
            {result ? (
                <div className="px-4 py-5 grid gap-3">
                    <p className="flex items-center gap-2 text-[13px] font-medium text-slate-900">
                        <CheckCircle2Icon className="size-4 text-emerald-500" />
                        {(result.created + result.updated).toLocaleString()} contacts from LinkedIn
                    </p>
                    <dl className="grid grid-cols-3 gap-2">
                        {(
                            [
                                ["New", result.created],
                                ["Updated", result.updated],
                                ["Skipped", result.skipped],
                            ] as const
                        ).map(([label, n]) => (
                            <div key={label} className="rounded-md border border-slate-200 px-3 py-2">
                                <dt className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">{label}</dt>
                                <dd className="text-[15px] font-semibold text-slate-900 tabular-nums">{n.toLocaleString()}</dd>
                            </div>
                        ))}
                    </dl>
                    <NoEmailNote count={result.no_email ?? 0} />
                    <p className="text-[11.5px] text-slate-500">Each contact keeps its LinkedIn profile, so LinkedIn steps and the email finder can use it.</p>
                </div>
            ) : (
                <div className="px-4 py-4 space-y-4">
                    <p className="text-[12px] leading-relaxed text-slate-600">
                        Each person becomes a contact with their name, title, company and LinkedIn profile. People who are already contacts are matched by their LinkedIn profile.
                    </p>
                    <section className="rounded-md border border-slate-200 divide-y divide-slate-100">
                        <div className="px-4 pt-3 pb-2">
                            <h2 className="text-[12.5px] font-medium text-slate-900">Add them to</h2>
                            <p className="text-[11.5px] text-slate-500">Optional. Every contact from this selection gets these.</p>
                        </div>
                        <Row icon={FolderTreeIcon} label="Segments" hint="A list you can send a campaign to">
                            <SegmentMultiPicker value={segmentIds} onChange={setSegmentIds} allowCreate suggestedName={suggestedSegment} placeholder="Pick or create a segment…" />
                        </Row>
                        <Row icon={TagsIcon} label="Labels" hint="To filter and segment by">
                            <CategoryPicker value={categoryIds} onChange={setCategoryIds} />
                        </Row>
                        <Row icon={MegaphoneIcon} label="Campaigns" hint="Enrolled as leads; an active campaign starts on them">
                            <CampaignMultiPicker value={campaignIds} onChange={setCampaignIds} />
                        </Row>
                    </section>
                    <section className="rounded-md border border-slate-200">
                        <div className="flex items-start gap-3 px-4 py-3">
                            <div className="min-w-0 flex-1">
                                <p className="text-[12.5px] font-medium text-slate-900">Skip people who are already contacts</p>
                                <p className="text-[11.5px] text-slate-500">{skipExisting ? "They are left out entirely." : "Empty details are filled in from LinkedIn. Nothing they already have is erased."}</p>
                            </div>
                            <Toggle value={skipExisting} onChange={setSkipExisting} ariaLabel="Skip people who are already contacts" />
                        </div>
                        <div className="flex items-start gap-3 border-t border-slate-100 px-4 py-3">
                            <div className="min-w-0 flex-1">
                                <p className="text-[12.5px] font-medium text-slate-900">Find work emails</p>
                                <p className="text-[11.5px] text-slate-500">
                                    {findEmails
                                        ? `Looks up a work email for each person in the background, at ${emailCost != null ? `${fmtUsdCost(emailCost)} per email found` : "the lead finder's price per email found"}, from your balance. You can keep working while it runs.`
                                        : "People without an email on their profile are left out."}
                                </p>
                            </div>
                            <Toggle value={findEmails} onChange={setFindEmails} ariaLabel="Find work emails" />
                        </div>
                    </section>
                </div>
            )}
        </LeadDialog>
    );
}

function NoEmailNote({ count }: { count: number }) {
    if (count <= 0) return null;
    return (
        <p className="flex items-start gap-2 rounded-md bg-amber-50 px-3 py-2 text-[11.5px] leading-relaxed text-amber-800 ring-1 ring-amber-200">
            <CircleAlertIcon className="mt-0.5 size-3.5 shrink-0" />
            {count === 1 ? "1 person had no email" : `${count.toLocaleString()} people had no email`}; turn on Find work emails to look them up.
        </p>
    );
}

const IMPORT_TOAST = (id: string) => `linkedin-import:${id}`;

function showImportProgress(importId: string) {
    toast.custom(<ImportProgressToast importId={importId} />, { id: IMPORT_TOAST(importId), duration: Infinity, position: "bottom-right" });
}

// A background import's progress, then its result; dismissed by hand.
function ImportProgressToast({ importId }: { importId: string }) {
    const { data: imp, isError } = useLinkedInImport(importId);
    const done = importFinished(imp);
    const failed = imp?.status === "failed";
    const total = imp?.total ?? 0;
    const pct = total > 0 ? Math.min(100, Math.round(((imp?.processed ?? 0) / total) * 100)) : 0;
    const added = (imp?.created ?? 0) + (imp?.updated ?? 0);
    return (
        <div role="status" className="w-[320px] max-w-[calc(100vw-2rem)] rounded-lg bg-white px-3 py-2.5 text-slate-800 shadow-[0_3px_10px_rgb(0_0_0/0.1),0_3px_3px_rgb(0_0_0/0.05)] ring-1 ring-slate-200">
            <div className="flex items-center gap-2">
                {failed || isError ? (
                    <CircleAlertIcon className="size-4 shrink-0 text-rose-500" />
                ) : done ? (
                    <CheckCircle2Icon className="size-4 shrink-0 text-emerald-500" />
                ) : (
                    <Loader2Icon className="size-4 shrink-0 animate-spin text-sky-600" />
                )}
                <p className="min-w-0 flex-1 truncate text-[12.5px] font-medium">
                    {failed ? "The LinkedIn import stopped" : done ? `${added.toLocaleString()} contact${added === 1 ? "" : "s"} from LinkedIn` : "Adding people from LinkedIn"}
                </p>
                <button type="button" aria-label="Dismiss" onClick={() => toast.dismiss(IMPORT_TOAST(importId))} className="-mr-1 inline-flex size-6 shrink-0 items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700">
                    <XIcon className="size-3.5" />
                </button>
            </div>
            {!done && !isError && (
                <div className="mt-2">
                    <div className="h-1.5 overflow-hidden rounded-full bg-slate-100">
                        <div className="h-full rounded-full bg-sky-500 transition-[width] duration-500" style={{ width: `${pct}%` }} />
                    </div>
                    <p className="mt-1 text-[11px] tabular-nums text-slate-500">
                        {total > 0 ? `${(imp?.processed ?? 0).toLocaleString()} of ${total.toLocaleString()}` : "Starting"}
                        {imp && imp.emails_found > 0 ? `, ${imp.emails_found.toLocaleString()} emails found` : ""}
                    </p>
                </div>
            )}
            {isError && !imp && <p className="mt-1 text-[11.5px] text-slate-500">Its progress could not be read. The contacts still arrive as it finishes.</p>}
            {failed && imp?.error && <p className="mt-1 text-[11.5px] text-slate-500">{imp.error}</p>}
            {done && imp && (
                <>
                    <p className="mt-1 text-[11.5px] leading-relaxed text-slate-500">
                        {imp.created.toLocaleString()} new, {imp.updated.toLocaleString()} updated, {imp.emails_found.toLocaleString()} emails found
                        {imp.skipped > 0 ? `, ${imp.skipped.toLocaleString()} skipped` : ""}.
                    </p>
                    {imp.no_email > 0 && <p className="mt-1 text-[11.5px] text-amber-700">{imp.no_email === 1 ? "1 person had" : `${imp.no_email.toLocaleString()} people had`} no work email to find and {imp.no_email === 1 ? "was" : "were"} not added.</p>}
                    <Link to="/app/contacts" onClick={() => toast.dismiss(IMPORT_TOAST(importId))} className="mt-1.5 inline-flex items-center gap-1 text-[11.5px] font-medium text-sky-700 hover:underline">
                        Open contacts
                        <ArrowRightIcon className="size-3" />
                    </Link>
                </>
            )}
        </div>
    );
}

function Row({ icon: Icon, label, hint, children }: { icon: LucideIcon; label: string; hint: string; children: ReactNode }) {
    return (
        <div className="px-4 py-3 grid gap-1.5 sm:grid-cols-[150px_1fr] sm:gap-3">
            <div className="flex items-start gap-2">
                <Icon className="size-3.5 mt-0.5 text-slate-400" />
                <div>
                    <div className="text-[12px] font-medium text-slate-800">{label}</div>
                    <div className="text-[11px] leading-snug text-slate-500">{hint}</div>
                </div>
            </div>
            <div className="min-w-0">{children}</div>
        </div>
    );
}

export function PasteUrlDialog({ open, onClose, onLoad }: { open: boolean; onClose: () => void; onLoad: (q: FinderQuery) => void }) {
    const parse = useParseFinderUrl();
    const [url, setUrl] = useState("");
    const [parsed, setParsed] = useState<ParsedUrl | null>(null);
    useEffect(() => {
        if (open) return;
        setUrl("");
        setParsed(null);
        parse.reset();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open]);
    const filterCount = parsed ? Object.keys(parsed.query.filters ?? {}).length : 0;
    const apply = (exact: boolean) => {
        if (!parsed) return;
        const q = { ...parsed.query };
        if (!exact) delete q.url;
        onLoad(q);
        onClose();
    };
    const exactOnly = !!parsed?.query.url && filterCount === 0 && !parsed.query.keywords;
    return (
        <LeadDialog
            open={open}
            onClose={onClose}
            icon={<ClipboardPasteIcon className="size-3" />}
            eyebrow="LinkedIn"
            title="Paste a LinkedIn search"
            footer={
                parsed ? (
                    <>
                        {parsed.query.url && !exactOnly && (
                            <button type="button" onClick={() => apply(true)} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900">
                                Run it exactly
                            </button>
                        )}
                        <button type="button" onClick={() => apply(exactOnly)} className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12px] font-medium hover:bg-sky-700">
                            {exactOnly ? "Run this search" : "Use these filters"}
                        </button>
                    </>
                ) : (
                    <>
                        <button type="button" onClick={onClose} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900">
                            Cancel
                        </button>
                        <button
                            type="button"
                            disabled={!url.trim() || parse.isPending}
                            onClick={() => parse.mutate(url.trim(), { onSuccess: setParsed })}
                            className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                        >
                            {parse.isPending && <Loader2Icon className="size-3.5 animate-spin" />}
                            Read the search
                        </button>
                    </>
                )
            }
        >
            <div className="px-4 py-4 grid gap-3">
                <p className="text-[12px] text-slate-600">Copy the address of a LinkedIn people search, a Sales Navigator lead or account search, or a Recruiter search. Warmbly turns it into filters you can edit.</p>
                <textarea
                    autoFocus
                    rows={3}
                    value={url}
                    onChange={(e) => {
                        setUrl(e.target.value);
                        setParsed(null);
                    }}
                    placeholder="https://www.linkedin.com/sales/search/people?query=…"
                    aria-label="LinkedIn search URL"
                    className="w-full rounded-md border border-slate-200 bg-white px-2.5 py-2 font-mono text-[12px] text-slate-900 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 placeholder:text-slate-400"
                />
                {parse.isError && <p className="text-[12px] text-red-600">{socialErrorMessage(parse.error)}</p>}
                {parsed && (
                    <div className="grid gap-1.5 rounded-md border border-slate-200 bg-slate-50/60 p-3 text-[12px]">
                        <p className="flex items-center gap-1.5 font-medium text-slate-900">
                            <CheckCircle2Icon className="size-4 text-emerald-500" />
                            {SOURCE_LABELS[parsed.query.source] ?? parsed.query.source}: {filterCount} filter{filterCount === 1 ? "" : "s"}
                            {parsed.query.keywords ? ` and keywords "${parsed.query.keywords}"` : ""}
                        </p>
                        {exactOnly && <p className="text-slate-500">This search runs exactly as LinkedIn has it; its filters stay on LinkedIn's side.</p>}
                        {!!parsed.skipped?.length && (
                            <p className="text-slate-500">
                                Not shown as filters: {parsed.skipped.join(", ")}. Run it exactly to keep them.
                            </p>
                        )}
                    </div>
                )}
            </div>
        </LeadDialog>
    );
}

export function SaveSearchDialog({ open, onClose, query, suggestedName, editing, onSaved }: { open: boolean; onClose: () => void; query: FinderQuery; suggestedName: string; editing: SavedSearch | null; onSaved: (s: SavedSearch) => void }) {
    const create = useCreateSavedSearch();
    const patch = usePatchSavedSearch();
    const [name, setName] = useState("");
    useEffect(() => {
        if (open) setName(editing?.name ?? suggestedName.slice(0, 120));
    }, [open, editing, suggestedName]);
    const pending = create.isPending || patch.isPending;
    const save = async (asNew: boolean) => {
        const n = name.trim();
        if (!n) return;
        try {
            if (editing && !asNew) {
                const s = await patch.mutateAsync({ id: editing.id, body: { name: n, query } });
                onSaved(s);
            } else {
                const s = await create.mutateAsync({ name: n, query });
                onSaved(s);
            }
            toast.success("Search saved");
            onClose();
        } catch {
            // The hook already said why.
        }
    };
    return (
        <LeadDialog
            open={open}
            onClose={onClose}
            width={460}
            icon={<BookmarkIcon className="size-3" />}
            eyebrow="Saved search"
            title={editing ? `Update ${editing.name}` : "Save this search"}
            footer={
                <>
                    {editing && (
                        <button type="button" disabled={pending || !name.trim()} onClick={() => save(true)} className="h-7 px-3 rounded-md text-[12px] text-slate-600 hover:text-slate-900 disabled:opacity-50">
                            Save as new
                        </button>
                    )}
                    <button
                        type="button"
                        disabled={pending || !name.trim()}
                        onClick={() => save(false)}
                        className="h-7 px-3 rounded-md bg-sky-600 text-white text-[12px] font-medium inline-flex items-center gap-1.5 hover:bg-sky-700 disabled:opacity-60"
                    >
                        {pending && <Loader2Icon className="size-3.5 animate-spin" />}
                        {editing ? "Update" : "Save"}
                    </button>
                </>
            }
        >
            <form
                className="px-4 py-4 grid gap-2"
                onSubmit={(e) => {
                    e.preventDefault();
                    void save(false);
                }}
            >
                <label className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Name</label>
                <TextInput value={name} onChange={setName} autoFocus placeholder="Heads of growth in Germany" />
                <p className="text-[11.5px] text-slate-500">Everyone in the workspace sees saved searches. Opening one runs it again with today's results.</p>
            </form>
        </LeadDialog>
    );
}

export function SavedSearchesMenu({ onOpen, onEdit, currentId }: { onOpen: (s: SavedSearch) => void; onEdit: (s: SavedSearch) => void; currentId?: string }) {
    const saved = useSavedSearches();
    const del = useDeleteSavedSearch();
    const confirm = useConfirm();
    const [open, setOpen] = useState(false);
    const list = saved.data ?? [];
    return (
        <PopoverMenu align="end" open={open} onOpenChange={setOpen}>
            <PopoverMenuTrigger asChild>
                <button type="button" className="h-7 px-2 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5" aria-label="Saved searches">
                    <BookmarkIcon className="size-3.5" />
                    <span className="max-sm:hidden">Saved</span>
                    {list.length > 0 && <span className="text-slate-400 tabular-nums">{list.length}</span>}
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent role="dialog" aria-label="Saved searches" minWidth={300} className="max-h-[60vh]">
                <PopoverMenuLabel>Saved LinkedIn searches</PopoverMenuLabel>
                {saved.isPending && <p className="px-3 py-2 text-[12px] text-slate-500">Loading…</p>}
                {!saved.isPending && list.length === 0 && <p className="px-3 py-2 text-[12px] text-slate-500">Nothing saved yet. Build a search, then save it from the selection bar or the results header.</p>}
                {list.map((s) => (
                    <div key={s.id} className={cn("group flex items-center gap-1 px-1.5", s.id === currentId && "bg-sky-50/60")}>
                        <button
                            type="button"
                            onClick={() => {
                                setOpen(false);
                                onOpen(s);
                            }}
                            className="min-w-0 flex-1 text-left px-1.5 py-1.5 rounded hover:bg-slate-50"
                        >
                            <span className="block truncate text-[12.5px] text-slate-800">{s.name}</span>
                            <span className="block truncate text-[11px] text-slate-400">{SOURCE_LABELS[s.query.source] ?? s.query.source}</span>
                        </button>
                        <button
                            type="button"
                            aria-label={`Rename ${s.name}`}
                            onClick={() => {
                                setOpen(false);
                                onEdit(s);
                            }}
                            className="size-6 rounded text-slate-400 hover:text-slate-700 hover:bg-slate-100 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus-visible:opacity-100"
                        >
                            <PencilIcon className="size-3" />
                        </button>
                        <button
                            type="button"
                            aria-label={`Delete ${s.name}`}
                            onClick={() => confirm.show(`Delete the saved search "${s.name}"? It is removed for everyone in the workspace.`, async () => void (await del.mutateAsync(s.id).catch(() => undefined)), undefined, { title: "Delete saved search", confirmLabel: "Delete", tone: "danger" })}
                            className="size-6 rounded text-slate-400 hover:text-red-600 hover:bg-red-50 inline-flex items-center justify-center opacity-100 md:opacity-0 md:group-hover:opacity-100 focus-visible:opacity-100"
                        >
                            <Trash2Icon className="size-3" />
                        </button>
                    </div>
                ))}
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
