// A store domain's DNS records in its drawer: list, add, edit and delete, with Warmbly's own records locked,
// and a check of what public DNS serves for each one right now.
import React from "react";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import toast from "react-hot-toast/headless";
import { CheckIcon, LockIcon, PencilIcon, PlusIcon, RefreshCwIcon, Trash2Icon, XIcon } from "lucide-react";
import { FieldError, NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu } from "@/components/ui/select-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useConfirm } from "@/hooks/context/confirm";
import { useAddDnsRecord, useCheckDnsRecords, useDeleteDnsRecord, usePatchDnsRecord } from "@/lib/api/hooks/app/store/useStore";
import type { DnsCheckResult, DnsRecord, StoreDomain } from "@/lib/api/models/app/store/Store";
import { storeErrorMessage } from "@/lib/buy/errors";
import { cn } from "@/lib/utils";
import { PressButton } from "./Press";

const TYPES = ["A", "AAAA", "CNAME", "MX", "TXT", "SRV", "CAA"] as const;
const HAS_PRIORITY = new Set(["MX", "SRV"]);
const DEFAULT_TTL = 3600;
// Clamped on save, not while typing, so a TTL can be typed digit by digit.
const clampTtl = (n: number) => Math.max(60, Math.min(86400, Math.round(Number.isFinite(n) ? n : DEFAULT_TTL)));

const PLACEHOLDER: Record<string, string> = {
    A: "192.0.2.10",
    AAAA: "2001:db8::10",
    CNAME: "target.example.com",
    MX: "mail.example.com",
    TXT: "v=spf1 include:example.com ~all",
    SRV: "10 5060 sip.example.com",
    CAA: '0 issue "letsencrypt.org"',
};

interface Draft {
    type: string;
    name: string;
    content: string;
    ttl: number;
    priority: number;
}

const blank = (): Draft => ({ type: "TXT", name: "", content: "", ttl: DEFAULT_TTL, priority: 10 });

export default function DnsRecords({ domain: d, loading, canEdit }: { domain: StoreDomain; loading: boolean; canEdit: boolean }) {
    // A manual connection leaves the zone at the customer's DNS provider, so records change there.
    const hosted = !(d.source === "connected" && d.connection?.method === "manual");
    const editable = canEdit && hosted;
    const records = d.dns ?? [];
    const check = useCheckDnsRecords();
    const [checked, setChecked] = React.useState<DnsCheckResult | null>(null);
    const [adding, setAdding] = React.useState(false);
    const [editing, setEditing] = React.useState<string | null>(null);
    const status = React.useMemo(() => new Map((checked?.records ?? []).map((r) => [r.id, r.status])), [checked]);

    const runCheck = () => check.mutate(d.id, { onSuccess: setChecked, onError: () => setChecked(null) });

    return (
        <section className="px-5 py-4 space-y-2.5">
            <div className="flex items-center gap-2">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium flex-1">DNS records</div>
                <PressButton tone="ghost" className="h-6 px-1.5 text-[11.5px]" disabled={check.isPending || records.length === 0} onClick={runCheck}>
                    <RefreshCwIcon className={cn("w-3 h-3", check.isPending && "animate-spin")} /> {check.isPending ? "Checking…" : "Check records"}
                </PressButton>
                {editable && !adding && (
                    <PressButton tone="secondary" className="h-6 px-2 text-[11.5px]" onClick={() => setAdding(true)}>
                        <PlusIcon className="w-3 h-3" /> Add record
                    </PressButton>
                )}
            </div>

            {check.isError && <FieldError message={storeErrorMessage(check.error)} />}
            {checked && (
                <p className="text-[11px] text-slate-500">
                    {checked.records.every((r) => r.status === "ok") ? "Every record is live. " : `${checked.records.filter((r) => r.status !== "ok").length} not seen in public DNS yet. `}
                    {checked.nameservers.length > 0 && (
                        <>
                            Answered by <span className="font-mono">{checked.nameservers.join(", ")}</span>.
                        </>
                    )}
                </p>
            )}

            <AnimatePresence initial={false}>
                {adding && (
                    <motion.div key="add" initial={{ opacity: 0, height: 0 }} animate={{ opacity: 1, height: "auto" }} exit={{ opacity: 0, height: 0 }} className="overflow-hidden">
                        <AddForm domainId={d.id} domainName={d.name} onDone={() => setAdding(false)} />
                    </motion.div>
                )}
            </AnimatePresence>

            {loading ? (
                <div className="space-y-1.5">
                    {Array.from({ length: 4 }).map((_, i) => (
                        <div key={i} className="h-8 rounded skeleton-shimmer" />
                    ))}
                </div>
            ) : records.length === 0 ? (
                <p className="text-[11.5px] text-slate-500">No records to show yet.</p>
            ) : (
                <ul className="rounded-md border border-slate-200 divide-y divide-slate-100">
                    <AnimatePresence initial={false}>
                        {records.map((r) =>
                            editing === r.id ? (
                                <EditRow key={r.id} domainId={d.id} r={r} onDone={() => setEditing(null)} />
                            ) : (
                                <RecordRow key={r.id} domainId={d.id} r={r} editable={editable} status={status.get(r.id)} onEdit={() => setEditing(r.id)} />
                            ),
                        )}
                    </AnimatePresence>
                </ul>
            )}

            <p className="text-[11px] text-slate-400 flex items-start gap-1">
                <LockIcon className="w-3 h-3 mt-px shrink-0" />
                {hosted
                    ? "Locked records keep mail and the forward working. Warmbly looks after them."
                    : "Your DNS provider holds this domain's records. Add or change them there; locked ones are the ones mail needs."}
            </p>
        </section>
    );
}

function RecordRow({ domainId, r, editable, status, onEdit }: { domainId: string; r: DnsRecord; editable: boolean; status?: string; onEdit: () => void }) {
    const reduced = useReducedMotion();
    const confirm = useConfirm();
    const del = useDeleteDnsRecord();
    const remove = () =>
        confirm.show(
            `Delete the ${r.type} record ${r.name}? Anything relying on it stops working once the change spreads.`,
            async () => {
                await del.mutateAsync({ domainId, recordId: r.id }).catch(() => undefined);
            },
            undefined,
            { title: "Delete record", confirmLabel: "Delete record" },
        );
    return (
        <motion.li
            layout={!reduced}
            initial={reduced ? false : { opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0, height: 0 }}
            className="group pl-2.5 pr-1.5 py-1.5 flex items-center gap-2 min-w-0"
        >
            <span className="w-11 shrink-0 font-mono text-[10.5px] text-slate-500">{r.type}</span>
            <span className="w-20 sm:w-24 shrink-0 font-mono text-[11px] text-slate-800 truncate" title={r.name}>
                {r.name}
            </span>
            <span className="flex-1 min-w-0 font-mono text-[11px] text-slate-500 truncate" title={r.content}>
                {r.priority !== null && r.priority !== undefined && HAS_PRIORITY.has(r.type) ? `${r.priority} ` : ""}
                {r.content}
            </span>
            {status && (
                <span
                    className={cn(
                        "shrink-0 h-[18px] px-1.5 rounded-full border text-[10px] font-medium inline-flex items-center gap-0.5",
                        status === "ok" ? "bg-emerald-50 border-emerald-100 text-emerald-700" : "bg-amber-50 border-amber-200 text-amber-800",
                    )}
                >
                    {status === "ok" ? <CheckIcon className="w-2.5 h-2.5" /> : null}
                    {status === "ok" ? "Live" : "Missing"}
                </span>
            )}
            {r.managed ? (
                <Tooltip>
                    <TooltipTrigger asChild>
                        <span tabIndex={0} className="size-6 shrink-0 inline-flex items-center justify-center text-slate-400 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-sky-200">
                            <LockIcon className="w-3 h-3" />
                            <span className="sr-only">Locked</span>
                        </span>
                    </TooltipTrigger>
                    <TooltipContent side="left">Warmbly keeps this record for mail delivery, so it is locked.</TooltipContent>
                </Tooltip>
            ) : editable ? (
                <span className="shrink-0 flex items-center opacity-100 md:opacity-0 md:group-hover:opacity-100 md:group-focus-within:opacity-100 transition-opacity">
                    <button
                        type="button"
                        onClick={onEdit}
                        aria-label={`Edit ${r.type} record ${r.name}`}
                        className="size-6 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-800 hover:bg-slate-100 transition-colors"
                    >
                        <PencilIcon className="w-3 h-3" />
                    </button>
                    <button
                        type="button"
                        onClick={remove}
                        aria-label={`Delete ${r.type} record ${r.name}`}
                        className="size-6 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-red-600 hover:bg-red-50 transition-colors"
                    >
                        <Trash2Icon className="w-3 h-3" />
                    </button>
                </span>
            ) : (
                <span className="size-6 shrink-0" />
            )}
        </motion.li>
    );
}

/** Fields shared by the add form and the inline edit; the type is fixed once a record exists. */
function Fields({ draft, set, typeFixed, idPrefix }: { draft: Draft; set: (patch: Partial<Draft>) => void; typeFixed: boolean; idPrefix: string }) {
    const priority = HAS_PRIORITY.has(draft.type);
    return (
        <div className="grid grid-cols-2 sm:grid-cols-[88px_minmax(0,1fr)] gap-2">
            {typeFixed ? (
                <span className="h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 font-mono text-[12px] text-slate-600 inline-flex items-center">{draft.type}</span>
            ) : (
                <SelectMenu
                    value={draft.type}
                    onChange={(type) => set({ type })}
                    options={TYPES.map((t) => ({ value: t, label: t }))}
                    minWidth={120}
                    aria-label="Record type"
                />
            )}
            <TextInput id={`${idPrefix}-name`} value={draft.name} onChange={(name) => set({ name })} placeholder="@ or a subdomain" className="w-full" />
            <TextInput
                id={`${idPrefix}-content`}
                value={draft.content}
                onChange={(content) => set({ content })}
                placeholder={PLACEHOLDER[draft.type] ?? "Value"}
                className="col-span-2 w-full font-mono"
            />
            <div className="col-span-2 flex flex-wrap items-center gap-2">
                <label className="inline-flex items-center gap-1.5 text-[11px] text-slate-500">
                    TTL
                    <NumberInput value={draft.ttl} onChange={(ttl) => set({ ttl })} step={60} suffix="s" className="w-28" />
                </label>
                {priority && (
                    <label className="inline-flex items-center gap-1.5 text-[11px] text-slate-500">
                        Priority
                        <NumberInput value={draft.priority} onChange={(p) => set({ priority: p })} min={0} max={65535} className="w-20" />
                    </label>
                )}
            </div>
        </div>
    );
}

function AddForm({ domainId, domainName, onDone }: { domainId: string; domainName: string; onDone: () => void }) {
    const add = useAddDnsRecord();
    const [draft, setDraft] = React.useState<Draft>(blank);
    const [error, setError] = React.useState<string | null>(null);
    const set = (patch: Partial<Draft>) => {
        setError(null);
        setDraft((d) => ({ ...d, ...patch }));
    };
    const ready = draft.content.trim() !== "";
    const save = async () => {
        if (!ready || add.isPending) return;
        try {
            await add.mutateAsync({
                domainId,
                record: {
                    type: draft.type,
                    name: draft.name.trim() || "@",
                    content: draft.content.trim(),
                    ttl: clampTtl(draft.ttl),
                    ...(HAS_PRIORITY.has(draft.type) ? { priority: draft.priority } : {}),
                },
            });
            onDone();
        } catch (e) {
            setError(storeErrorMessage(e));
        }
    };
    return (
        <div
            className="rounded-md border border-sky-200 bg-sky-50/30 p-2.5 space-y-2"
            onKeyDown={(e) => {
                if (e.key === "Escape" && !document.querySelector("[data-floating]")) {
                    e.stopPropagation();
                    onDone();
                } else if (e.key === "Enter" && (e.target as HTMLElement).tagName === "INPUT") {
                    e.preventDefault();
                    void save();
                }
            }}
        >
            <div className="text-[11.5px] font-medium text-slate-800">New record on {domainName}</div>
            <Fields draft={draft} set={set} typeFixed={false} idPrefix="dns-add" />
            <FieldError message={error} />
            <div className="flex items-center gap-2">
                <PressButton disabled={!ready || add.isPending} onClick={() => void save()}>
                    {add.isPending ? "Adding…" : "Add record"}
                </PressButton>
                <PressButton tone="ghost" onClick={onDone}>
                    Cancel
                </PressButton>
            </div>
        </div>
    );
}

function EditRow({ domainId, r, onDone }: { domainId: string; r: DnsRecord; onDone: () => void }) {
    const patch = usePatchDnsRecord();
    const [draft, setDraft] = React.useState<Draft>({ type: r.type, name: r.name, content: r.content, ttl: r.ttl || DEFAULT_TTL, priority: r.priority ?? 10 });
    const set = (p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p }));
    const save = () => {
        const next = {
            name: draft.name.trim() || "@",
            content: draft.content.trim(),
            ttl: clampTtl(draft.ttl),
            ...(HAS_PRIORITY.has(r.type) ? { priority: draft.priority } : {}),
        };
        if (!next.content) return;
        const changed =
            next.name !== r.name || next.content !== r.content || next.ttl !== r.ttl || (HAS_PRIORITY.has(r.type) && next.priority !== r.priority);
        if (!changed) return onDone();
        // Optimistic: the row shows the edit at once and goes back, with the reason, if it is refused.
        patch.mutate({ domainId, recordId: r.id, patch: next }, { onError: (e) => toast.error(storeErrorMessage(e)) });
        onDone();
    };
    return (
        <motion.li
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            className="p-2.5 bg-slate-50/60 space-y-2"
            onKeyDown={(e) => {
                if (e.key === "Escape" && !document.querySelector("[data-floating]")) {
                    e.stopPropagation();
                    onDone();
                } else if (e.key === "Enter" && (e.target as HTMLElement).tagName === "INPUT") {
                    e.preventDefault();
                    save();
                }
            }}
        >
            <Fields draft={draft} set={set} typeFixed idPrefix={`dns-${r.id}`} />
            <div className="flex items-center gap-2">
                <PressButton disabled={!draft.content.trim()} onClick={save}>
                    Save
                </PressButton>
                <PressButton tone="ghost" onClick={onDone}>
                    <XIcon className="w-3 h-3" /> Cancel
                </PressButton>
            </div>
        </motion.li>
    );
}
