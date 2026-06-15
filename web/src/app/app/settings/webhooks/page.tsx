// Webhooks — a Settings section. Workspaces register HTTPS endpoints here and
// Warmbly POSTs an HMAC-signed callback whenever a subscribed event fires. Each
// endpoint is issued a signing secret once (verify it by replying 2xx to a test
// event), subscribes to a set of event types, and keeps a per-attempt delivery
// log you can filter and redeliver from. High-volume "firehose" events are only
// sent when explicitly selected, and throttled drops surface as an info strip.

import React from "react";
import { createPortal } from "react-dom";
import toast from "react-hot-toast";
import { AnimatePresence, motion } from "framer-motion";
import {
    ActivityIcon,
    AlertTriangleIcon,
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    CheckIcon,
    ChevronDownIcon,
    ChevronRightIcon,
    CopyIcon,
    GaugeIcon,
    Loader2Icon,
    PlusIcon,
    RefreshCwIcon,
    SendIcon,
    Settings2Icon,
    ShieldCheckIcon,
    Trash2Icon,
    XIcon,
    type LucideIcon,
} from "lucide-react";

import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import { EmptyBlock } from "@/components/layout/Page";
import { Label, SearchInput, TextInput } from "@/components/ui/field";
import { cn } from "@/lib/utils";
import { useConfirm } from "@/hooks/context/confirm";
import { Toggle } from "../_components/SectionShell";
import { SectionShell } from "../_components/SectionShell";
import {
    useWebhooks,
    useCreateWebhook,
    useUpdateWebhook,
    useDeleteWebhook,
    useRotateWebhookSecret,
    useVerifyWebhook,
    useWebhookDeliveries,
    useRedeliverDelivery,
    useWebhookDrops,
} from "@/lib/api/hooks/app/webhooks/useWebhooks";
import listWebhookDeliveries from "@/lib/api/client/app/webhooks/listWebhookDeliveries";
import type {
    WebhookDelivery,
    WebhookDeliveryStatus,
    WebhookEndpoint,
    WebhookEndpointWithSecret,
    WebhookEventDescriptor,
} from "@/lib/api/models/app/webhooks/Webhook";

/* ── helpers ─────────────────────── */

function formatRelative(date: Date | string | undefined): string {
    if (!date) return "never";
    const d = typeof date === "string" ? new Date(date) : date;
    if (Number.isNaN(d.getTime())) return "never";
    const diff = Date.now() - d.getTime();
    if (diff < 0) return d.toLocaleString();
    const mins = Math.floor(diff / 60_000);
    if (mins < 1) return "just now";
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days < 7) return `${days}d ago`;
    return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

function CopyButton({ value, label }: { value: string; label?: string }) {
    const [copied, setCopied] = React.useState(false);
    return (
        <button
            type="button"
            onClick={async (e) => {
                e.stopPropagation();
                try {
                    await navigator.clipboard.writeText(value);
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                } catch {
                    /* clipboard blocked */
                }
            }}
            className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 h-7 text-[11.5px] text-slate-600 hover:bg-slate-50"
        >
            {copied ? <CheckIcon className="w-3.5 h-3.5 text-emerald-600" /> : <CopyIcon className="w-3.5 h-3.5" />}
            {copied ? "Copied" : (label ?? "Copy")}
        </button>
    );
}

/* ── event picker ─────────────────────── */

// EventPicker groups the catalog by category with checkbox-square rows and a
// search header. An empty selection means "all non-firehose events"; firehose
// events are flagged and only delivered when ticked explicitly.
function EventPicker({
    catalog,
    value,
    onChange,
}: {
    catalog: WebhookEventDescriptor[];
    value: string[];
    onChange: (next: string[]) => void;
}) {
    const [q, setQ] = React.useState("");
    const selected = new Set(value);

    const filtered = React.useMemo(() => {
        const needle = q.trim().toLowerCase();
        if (!needle) return catalog;
        return catalog.filter(
            (d) =>
                d.type.toLowerCase().includes(needle) ||
                d.category.toLowerCase().includes(needle) ||
                d.description.toLowerCase().includes(needle),
        );
    }, [catalog, q]);

    const grouped = React.useMemo(() => {
        const g: Record<string, WebhookEventDescriptor[]> = {};
        for (const d of filtered) (g[d.category] ??= []).push(d);
        return g;
    }, [filtered]);

    const toggle = (type: string) => {
        const next = new Set(selected);
        if (next.has(type)) next.delete(type);
        else next.add(type);
        onChange([...next]);
    };

    return (
        <div className="space-y-2.5">
            <div className="flex items-center gap-2">
                <SearchInput value={q} onChange={setQ} placeholder="Search events…" className="flex-1" />
                {value.length > 0 && (
                    <button
                        type="button"
                        onClick={() => onChange([])}
                        className="h-7 px-2.5 rounded-md border border-slate-200 text-[11.5px] text-slate-600 hover:bg-slate-50 shrink-0"
                    >
                        Subscribe to all
                    </button>
                )}
            </div>
            <div
                className={cn(
                    "rounded-md border px-2.5 py-2 text-[11.5px] leading-relaxed",
                    value.length === 0
                        ? "border-sky-200 bg-sky-50 text-sky-700"
                        : "border-slate-200 bg-slate-50 text-slate-500",
                )}
            >
                {value.length === 0 ? (
                    <>Subscribed to all events. New event types are included automatically (high-volume events excluded).</>
                ) : (
                    <>
                        Subscribed to <span className="font-medium">{value.length}</span>{" "}
                        {value.length === 1 ? "event" : "events"}. Leave none selected to receive all events.
                    </>
                )}
            </div>
            <div className="max-h-[280px] overflow-y-auto rounded-md border border-slate-200 divide-y divide-slate-100">
                {Object.keys(grouped).length === 0 ? (
                    <div className="px-3 py-6 text-center text-[11.5px] text-slate-400">No events match.</div>
                ) : (
                    Object.entries(grouped).map(([cat, list]) => (
                        <div key={cat} className="p-1.5">
                            <div className="px-1.5 py-1 text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                                {cat}
                            </div>
                            <div className="space-y-0.5">
                                {list.map((d) => {
                                    const on = selected.has(d.type);
                                    return (
                                        <button
                                            key={d.type}
                                            type="button"
                                            onClick={() => toggle(d.type)}
                                            className={cn(
                                                "w-full flex items-start gap-2 rounded-md px-1.5 py-1.5 text-left transition-colors",
                                                on ? "bg-sky-50" : "hover:bg-slate-50",
                                            )}
                                        >
                                            <span
                                                className={cn(
                                                    "mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded",
                                                    on ? "bg-sky-600 text-white" : "border border-slate-300",
                                                )}
                                            >
                                                {on && <CheckIcon className="w-3 h-3" />}
                                            </span>
                                            <span className="min-w-0 flex-1">
                                                <span className="flex items-center gap-1.5">
                                                    <span className="block text-[12px] font-medium text-slate-700 font-mono truncate">
                                                        {d.type}
                                                    </span>
                                                    {d.firehose && (
                                                        <span className="shrink-0 inline-flex items-center rounded-sm bg-amber-50 border border-amber-200 px-1 text-[9.5px] uppercase tracking-[0.08em] font-semibold text-amber-700">
                                                            High volume
                                                        </span>
                                                    )}
                                                </span>
                                                {d.description && (
                                                    <span className="block text-[11px] text-slate-400 leading-tight">{d.description}</span>
                                                )}
                                            </span>
                                        </button>
                                    );
                                })}
                            </div>
                        </div>
                    ))
                )}
            </div>
            <p className="text-[10.5px] text-amber-700">
                High-volume events are only sent if you select them explicitly.
            </p>
        </div>
    );
}

/* ── create wizard ─────────────────────── */

const WIZARD_STEPS = ["Endpoint", "Events", "Secret"] as const;

function Stepper({ step }: { step: number }) {
    return (
        <div className="px-4 pt-3 pb-1 shrink-0">
            <div className="flex items-center">
                {WIZARD_STEPS.map((label, i) => {
                    const done = i < step;
                    const active = i === step;
                    return (
                        <React.Fragment key={label}>
                            <div className="flex items-center gap-1.5">
                                <span
                                    className={cn(
                                        "flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold transition-colors",
                                        active ? "bg-sky-600 text-white" : done ? "bg-sky-100 text-sky-700" : "bg-slate-100 text-slate-400",
                                    )}
                                >
                                    {done ? <CheckIcon className="w-3 h-3" /> : i + 1}
                                </span>
                                <span
                                    className={cn(
                                        "text-[11px] font-medium hidden sm:inline transition-colors",
                                        active ? "text-slate-900" : done ? "text-slate-500" : "text-slate-400",
                                    )}
                                >
                                    {label}
                                </span>
                            </div>
                            {i < WIZARD_STEPS.length - 1 && (
                                <span className={cn("mx-2 h-px flex-1 transition-colors", i < step ? "bg-sky-200" : "bg-slate-200")} />
                            )}
                        </React.Fragment>
                    );
                })}
            </div>
        </div>
    );
}

function CreateModal({ catalog, onClose }: { catalog: WebhookEventDescriptor[]; onClose: () => void }) {
    const create = useCreateWebhook();

    const [step, setStep] = React.useState(0);
    const [url, setUrl] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [events, setEvents] = React.useState<string[]>([]);
    const [created, setCreated] = React.useState<WebhookEndpointWithSecret | null>(null);

    const urlValid = /^https:\/\/.+/i.test(url.trim());

    const goNext = () => {
        if (step === 0 && !urlValid) {
            toast.error("Enter a valid endpoint URL (https://…)");
            return;
        }
        setStep((s) => Math.min(s + 1, WIZARD_STEPS.length - 1));
    };
    const goBack = () => setStep((s) => Math.max(s - 1, 0));

    const submit = async () => {
        if (!urlValid) {
            toast.error("Enter a valid endpoint URL");
            return;
        }
        try {
            const endpoint = await create.mutateAsync({
                url: url.trim(),
                description: description.trim(),
                event_types: events,
                enabled: true,
            });
            setCreated(endpoint);
            setStep(WIZARD_STEPS.length - 1);
            toast.success("Endpoint created");
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not create the endpoint");
        }
    };

    return createPortal(
        <div className="fixed inset-0 z-[60] flex items-center justify-center p-4">
            <motion.div
                className="absolute inset-0 bg-slate-900/40"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.12, ease: "easeOut" }}
                onClick={onClose}
            />
            <motion.div
                className="relative w-full max-w-lg max-h-[90vh] flex flex-col overflow-hidden rounded-xl bg-white shadow-xl ring-1 ring-slate-200"
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: 20 }}
                transition={{ duration: 0.15, ease: "easeOut" }}
            >
                <div className="flex items-center border-b border-slate-200 px-4 h-11 shrink-0">
                    <span className="text-[12.5px] font-medium text-slate-900">
                        {created ? "Endpoint created" : "Add a webhook endpoint"}
                    </span>
                    <button onClick={onClose} className="ml-auto h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100">
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                {created ? (
                    <SecretReveal
                        title={`${created.url} is ready`}
                        secret={created.secret}
                        onDone={onClose}
                    />
                ) : (
                    <>
                        <Stepper step={step} />
                        <div className="relative flex-1 overflow-y-auto">
                            <AnimatePresence mode="wait" initial={false}>
                                <motion.div
                                    key={step}
                                    initial={{ opacity: 0, x: 24 }}
                                    animate={{ opacity: 1, x: 0 }}
                                    exit={{ opacity: 0, x: -24 }}
                                    transition={{ duration: 0.18, ease: "easeOut" }}
                                    className="p-4 space-y-3"
                                >
                                    {step === 0 && (
                                        <>
                                            <div>
                                                <Label>Endpoint URL</Label>
                                                <TextInput value={url} onChange={setUrl} placeholder="https://acme.com/webhooks/warmbly" className="w-full" />
                                                <p className="mt-1 text-[11px] text-slate-400">
                                                    Must be HTTPS. We POST a signed JSON body here for each subscribed event.
                                                </p>
                                            </div>
                                            <div>
                                                <Label>Description</Label>
                                                <TextInput value={description} onChange={setDescription} placeholder="What this endpoint is for (optional)" className="w-full" />
                                            </div>
                                        </>
                                    )}
                                    {step === 1 && (
                                        <div>
                                            <Label>Events</Label>
                                            <EventPicker catalog={catalog} value={events} onChange={setEvents} />
                                        </div>
                                    )}
                                </motion.div>
                            </AnimatePresence>
                        </div>

                        <div className="px-4 py-2.5 border-t border-slate-200 flex items-center gap-2 shrink-0">
                            {step > 0 ? (
                                <button onClick={goBack} className="h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-600 hover:bg-slate-50 inline-flex items-center gap-1.5">
                                    <ArrowLeftIcon className="w-3.5 h-3.5" /> Back
                                </button>
                            ) : (
                                <button onClick={onClose} className="h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-600 hover:bg-slate-50">
                                    Cancel
                                </button>
                            )}
                            <span className="ml-auto text-[11px] text-slate-400">
                                Step {step + 1} of {WIZARD_STEPS.length}
                            </span>
                            {step < 1 ? (
                                <button onClick={goNext} className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 inline-flex items-center gap-1.5">
                                    Next <ArrowRightIcon className="w-3.5 h-3.5" />
                                </button>
                            ) : (
                                <button
                                    onClick={submit}
                                    disabled={create.isPending}
                                    className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 disabled:opacity-60 inline-flex items-center gap-1.5"
                                >
                                    {create.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckIcon className="w-3.5 h-3.5" />}
                                    Create endpoint
                                </button>
                            )}
                        </div>
                    </>
                )}
            </motion.div>
        </div>,
        document.body,
    );
}

// SecretReveal — the one-time signing-secret display (amber reveal pattern).
function SecretReveal({ title, secret, onDone }: { title: string; secret: string; onDone: () => void }) {
    return (
        <div className="flex-1 overflow-y-auto p-4 space-y-3">
            <div className="flex items-center gap-2 text-emerald-600">
                <CheckCircle2Icon className="w-4 h-4" />
                <span className="text-[12.5px] font-medium truncate">{title}</span>
            </div>
            <p className="text-[12px] text-slate-500 leading-relaxed">
                Save the signing secret now. For security it is shown only once and cannot be retrieved later (you can rotate it
                if you lose it). Use it to verify the HMAC signature on every delivery.
            </p>
            <div>
                <Label>Signing secret</Label>
                <div className="flex items-center gap-1.5">
                    <code className="flex-1 truncate rounded-md border border-amber-200 bg-amber-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-amber-800">
                        {secret}
                    </code>
                    <CopyButton value={secret} />
                </div>
            </div>
            <div className="rounded-md border border-slate-200 bg-slate-50 px-2.5 py-2 text-[11px] text-slate-500 leading-relaxed">
                Verify your endpoint by replying with a 2xx to its first delivery. Send a test event from the endpoint's
                Overview tab once it is created.
            </div>
            <div className="pt-1">
                <button onClick={onDone} className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700">
                    Done
                </button>
            </div>
        </div>
    );
}

/* ── status badges ─────────────────────── */

function VerificationBadge({ endpoint }: { endpoint: WebhookEndpoint }) {
    if (endpoint.auto_disabled_at) {
        return (
            <span
                title={endpoint.disabled_reason || "Auto-disabled after repeated failures"}
                className="inline-flex items-center gap-1 rounded-sm bg-rose-50 border border-rose-200 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-rose-700"
            >
                <AlertTriangleIcon className="w-3 h-3" /> Auto-disabled
            </span>
        );
    }
    if (!endpoint.enabled) {
        return (
            <span className="inline-flex items-center rounded-sm bg-slate-100 border border-slate-200 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-slate-500">
                Disabled
            </span>
        );
    }
    if (endpoint.verified_at) {
        return (
            <span className="inline-flex items-center gap-1 rounded-sm bg-emerald-50 border border-emerald-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-emerald-700">
                <ShieldCheckIcon className="w-3 h-3" /> Verified
            </span>
        );
    }
    return (
        <span className="inline-flex items-center rounded-sm bg-amber-50 border border-amber-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-amber-700">
            Pending verification
        </span>
    );
}

const DELIVERY_TONE: Record<WebhookDeliveryStatus, string> = {
    delivered: "bg-emerald-50 text-emerald-700 border-emerald-100",
    pending: "bg-slate-100 text-slate-600 border-slate-200",
    in_flight: "bg-sky-50 text-sky-700 border-sky-100",
    failed: "bg-amber-50 text-amber-700 border-amber-100",
    abandoned: "bg-rose-50 text-rose-700 border-rose-100",
};

function DeliveryStatusBadge({ status }: { status: WebhookDeliveryStatus }) {
    return (
        <span
            className={cn(
                "inline-flex items-center rounded-sm border px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold",
                DELIVERY_TONE[status] ?? DELIVERY_TONE.pending,
            )}
        >
            {status.replace("_", " ")}
        </span>
    );
}

/* ── events summary popover ─────────────────────── */

function EventsSummary({ endpoint }: { endpoint: WebhookEndpoint }) {
    const [open, setOpen] = React.useState(false);
    const all = endpoint.event_types.length === 0;
    const label = all ? "All events" : `${endpoint.event_types.length} ${endpoint.event_types.length === 1 ? "event" : "events"}`;
    return (
        <span className="relative inline-block">
            <button
                type="button"
                onClick={(e) => {
                    e.stopPropagation();
                    if (!all) setOpen((o) => !o);
                }}
                className={cn(
                    "inline-flex items-center gap-1 rounded-sm bg-slate-100 px-1.5 py-0.5 text-[10.5px] text-slate-600",
                    !all && "hover:bg-slate-200",
                )}
            >
                {label}
                {!all && <ChevronDownIcon className="w-3 h-3" />}
            </button>
            <AnimatePresence>
                {open && !all && (
                    <>
                        <span className="fixed inset-0 z-30" onClick={(e) => { e.stopPropagation(); setOpen(false); }} />
                        <motion.div
                            initial={{ opacity: 0, y: -4 }}
                            animate={{ opacity: 1, y: 0 }}
                            exit={{ opacity: 0, y: -4 }}
                            transition={{ duration: 0.12 }}
                            className="absolute left-0 top-full z-40 mt-1 w-56 max-h-56 overflow-y-auto rounded-md border border-slate-200 bg-white p-1 shadow-lg"
                            onClick={(e) => e.stopPropagation()}
                        >
                            {endpoint.event_types.map((t) => (
                                <div key={t} className="px-1.5 py-1 text-[11px] font-mono text-slate-600 truncate">{t}</div>
                            ))}
                        </motion.div>
                    </>
                )}
            </AnimatePresence>
        </span>
    );
}

/* ── endpoint row ─────────────────────── */

function EndpointRow({ endpoint, onOpen }: { endpoint: WebhookEndpoint; onOpen: () => void }) {
    return (
        <button
            type="button"
            onClick={onOpen}
            className="w-full text-left rounded-lg border border-slate-200 p-3 hover:bg-slate-50/80 transition-colors"
        >
            <div className="flex items-start gap-3">
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 flex-wrap">
                        <code className="text-[12.5px] font-mono text-slate-800 truncate max-w-full">{endpoint.url}</code>
                        <VerificationBadge endpoint={endpoint} />
                        {endpoint.consecutive_failures > 0 && (
                            <span className="inline-flex items-center rounded-sm bg-rose-50 border border-rose-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-rose-700">
                                {endpoint.consecutive_failures} failing
                            </span>
                        )}
                    </div>
                    {endpoint.description && (
                        <div className="text-[11.5px] text-slate-400 truncate mt-0.5">{endpoint.description}</div>
                    )}
                    <div className="mt-1.5 flex items-center gap-2 flex-wrap text-[10.5px] text-slate-400">
                        <EventsSummary endpoint={endpoint} />
                        <span>Last success {formatRelative(endpoint.last_success_at)}</span>
                        {endpoint.auto_disabled_at && endpoint.disabled_reason && (
                            <span className="text-rose-500">{endpoint.disabled_reason}</span>
                        )}
                    </div>
                </div>
                <ChevronRightIcon className="w-4 h-4 text-slate-300 shrink-0 mt-0.5" />
            </div>
        </button>
    );
}

/* ── detail drawer ─────────────────────── */

const DRAWER_TABS: { key: string; label: string; icon: LucideIcon }[] = [
    { key: "overview", label: "Overview", icon: GaugeIcon },
    { key: "deliveries", label: "Deliveries", icon: ActivityIcon },
    { key: "settings", label: "Settings", icon: Settings2Icon },
];

function EndpointDrawer({
    endpoint,
    catalog,
    onClose,
}: {
    endpoint: WebhookEndpoint;
    catalog: WebhookEventDescriptor[];
    onClose: () => void;
}) {
    const [tab, setTab] = React.useState("overview");
    return (
        <AnimatePresence>
            <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.18 }}
                className="fixed inset-0 z-40 bg-slate-900/40"
                onClick={onClose}
            />
            <motion.aside
                initial={{ x: "100%" }}
                animate={{ x: 0 }}
                exit={{ x: "100%" }}
                transition={{ type: "spring", damping: 32, stiffness: 320 }}
                className="fixed right-0 top-0 z-50 h-full w-full sm:w-[600px] bg-white border-l border-slate-200 shadow-[0_0_60px_-12px_rgba(15,23,42,0.3)] flex flex-col"
            >
                {/* Header */}
                <div className="shrink-0 px-5 h-14 flex items-center gap-3 border-b border-slate-200">
                    <div className="w-8 h-8 rounded-lg bg-sky-50 text-sky-700 flex items-center justify-center shrink-0">
                        <ActivityIcon className="w-4 h-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                        <div className="text-[13px] font-medium text-slate-900 truncate font-mono">{endpoint.url}</div>
                        <div className="text-[10.5px] text-slate-400">Created {formatRelative(endpoint.created_at)}</div>
                    </div>
                    <VerificationBadge endpoint={endpoint} />
                    <button onClick={onClose} aria-label="Close" className="w-7 h-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 transition-colors shrink-0">
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                {/* Tabs */}
                <div className="shrink-0 px-3 flex items-center gap-1 border-b border-slate-200 overflow-x-auto">
                    {DRAWER_TABS.map((t) => {
                        const active = tab === t.key;
                        return (
                            <button
                                key={t.key}
                                onClick={() => setTab(t.key)}
                                className={cn(
                                    "relative h-10 px-2.5 inline-flex shrink-0 items-center gap-1.5 text-[12.5px] transition-colors",
                                    active ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-800",
                                )}
                            >
                                <t.icon className="w-3.5 h-3.5" />
                                {t.label}
                                {active && (
                                    <motion.span
                                        layoutId="webhook-tab-underline"
                                        className="absolute left-1.5 right-1.5 -bottom-px h-0.5 rounded-full bg-sky-600"
                                        transition={{ type: "spring", duration: 0.3, bounce: 0.15 }}
                                    />
                                )}
                            </button>
                        );
                    })}
                </div>

                {/* Body */}
                <div className="flex-1 min-h-0 overflow-y-auto">
                    {tab === "overview" && <OverviewTab endpoint={endpoint} />}
                    {tab === "deliveries" && <DeliveriesTab endpoint={endpoint} catalog={catalog} />}
                    {tab === "settings" && <SettingsTab endpoint={endpoint} catalog={catalog} onClose={onClose} />}
                </div>
            </motion.aside>
        </AnimatePresence>
    );
}

function OverviewTab({ endpoint }: { endpoint: WebhookEndpoint }) {
    const rotate = useRotateWebhookSecret();
    const verify = useVerifyWebhook();
    const confirm = useConfirm();
    const [secret, setSecret] = React.useState<string | null>(null);

    const onRotate = () =>
        confirm.show("Rotate this endpoint's signing secret? The current secret stops working immediately.", async () => {
            const res = await rotate.mutateAsync(endpoint.id);
            setSecret(res.secret);
        });

    const onTest = async () => {
        try {
            await verify.mutateAsync(endpoint.id);
            toast.success("Test sent. Your endpoint verifies on a 2xx.");
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not send the test event");
        }
    };

    return (
        <div className="p-5 space-y-5">
            <div>
                <Label>Endpoint URL</Label>
                <div className="flex items-center gap-1.5">
                    <code className="flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-slate-700">
                        {endpoint.url}
                    </code>
                    <CopyButton value={endpoint.url} />
                </div>
            </div>

            <div className="grid grid-cols-2 gap-3">
                <div className="rounded-md border border-slate-200 px-3 py-2.5">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Last success</div>
                    <div className="mt-1 text-[13px] text-slate-900">{formatRelative(endpoint.last_success_at)}</div>
                </div>
                <div className="rounded-md border border-slate-200 px-3 py-2.5">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Consecutive failures</div>
                    <div className={cn("mt-1 text-[13px]", endpoint.consecutive_failures > 0 ? "text-rose-600" : "text-slate-900")}>
                        {endpoint.consecutive_failures}
                    </div>
                </div>
            </div>

            {endpoint.last_failure_reason && (
                <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[11.5px] text-amber-800 leading-relaxed">
                    <span className="font-medium">Last failure</span> ({formatRelative(endpoint.last_failure_at)}):{" "}
                    {endpoint.last_failure_reason}
                </div>
            )}

            <div className="border-t border-slate-200 pt-4 space-y-2.5">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Verification</div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    {endpoint.verified_at
                        ? `Verified ${formatRelative(endpoint.verified_at)}.`
                        : "This endpoint isn't verified yet. Send a test event; it verifies on the first 2xx response."}
                </p>
                <button
                    type="button"
                    onClick={onTest}
                    disabled={verify.isPending}
                    className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 disabled:opacity-60 inline-flex items-center gap-1.5"
                >
                    {verify.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <SendIcon className="w-3.5 h-3.5" />}
                    Send test event
                </button>
            </div>

            <div className="border-t border-slate-200 pt-4 space-y-2.5">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Signing secret</div>
                <p className="text-[11.5px] text-slate-500 leading-relaxed">
                    The secret is shown only once at creation. Rotate it if it leaks; the new value is shown once below.
                </p>
                <button
                    type="button"
                    onClick={onRotate}
                    disabled={rotate.isPending}
                    className="h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-700 hover:bg-slate-50 disabled:opacity-60 inline-flex items-center gap-1.5"
                >
                    {rotate.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                    Rotate signing secret
                </button>
                {secret && (
                    <div className="rounded-md border border-amber-200 bg-amber-50 p-2">
                        <div className="text-[10.5px] uppercase tracking-[0.12em] text-amber-700 mb-1">New signing secret (shown once)</div>
                        <div className="flex items-center gap-1.5">
                            <code className="flex-1 truncate text-[11.5px] font-mono text-amber-800">{secret}</code>
                            <CopyButton value={secret} />
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}

const DELIVERY_STATUSES: WebhookDeliveryStatus[] = ["pending", "in_flight", "delivered", "failed", "abandoned"];

function DeliveriesTab({ endpoint, catalog }: { endpoint: WebhookEndpoint; catalog: WebhookEventDescriptor[] }) {
    const [status, setStatus] = React.useState<WebhookDeliveryStatus | "">("");
    const [eventType, setEventType] = React.useState("");

    // Live first page from the hook; extra pages are appended locally as the
    // user loads more (and the live page replaces the head on invalidation).
    const first = useWebhookDeliveries({ endpointId: endpoint.id, status, eventType, limit: 25 });
    const [extra, setExtra] = React.useState<WebhookDelivery[]>([]);
    const [cursor, setCursor] = React.useState<string | null>(null);
    const [hasMore, setHasMore] = React.useState(false);
    const [loadingMore, setLoadingMore] = React.useState(false);

    // Reset accumulation whenever the filter or the live head changes.
    React.useEffect(() => {
        setExtra([]);
        setCursor(first.data?.pagination.next_cursor ?? null);
        setHasMore(first.data?.pagination.has_more ?? false);
    }, [first.data, status, eventType]);

    const loadMore = async () => {
        if (!cursor) return;
        setLoadingMore(true);
        try {
            const res = await listWebhookDeliveries({
                endpointId: endpoint.id,
                status,
                eventType,
                cursor,
                limit: 25,
            });
            setExtra((prev) => [...prev, ...res.data]);
            setCursor(res.pagination.next_cursor);
            setHasMore(res.pagination.has_more);
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not load more deliveries");
        } finally {
            setLoadingMore(false);
        }
    };

    const rows = [...(first.data?.data ?? []), ...extra];

    return (
        <div className="p-4 space-y-3">
            <div className="flex items-center gap-2 flex-wrap">
                <select
                    value={status}
                    onChange={(e) => setStatus(e.target.value as WebhookDeliveryStatus | "")}
                    className="h-7 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-700 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                >
                    <option value="">All statuses</option>
                    {DELIVERY_STATUSES.map((s) => (
                        <option key={s} value={s}>{s.replace("_", " ")}</option>
                    ))}
                </select>
                <select
                    value={eventType}
                    onChange={(e) => setEventType(e.target.value)}
                    className="h-7 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-700 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100 max-w-[180px]"
                >
                    <option value="">All events</option>
                    {catalog.map((d) => (
                        <option key={d.type} value={d.type}>{d.type}</option>
                    ))}
                </select>
            </div>

            {first.isPending ? (
                <div className="py-10 text-center text-[12px] text-slate-400">Loading deliveries…</div>
            ) : rows.length === 0 ? (
                <EmptyBlock title="No deliveries yet" body="Once events fire, every delivery attempt to this endpoint shows here." />
            ) : (
                <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                    {rows.map((d) => (
                        <DeliveryRow key={d.id} delivery={d} />
                    ))}
                </div>
            )}

            {hasMore && (
                <div className="flex justify-center pt-1">
                    <button
                        type="button"
                        onClick={loadMore}
                        disabled={loadingMore}
                        className="h-8 px-3 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50 disabled:opacity-60 inline-flex items-center gap-1.5"
                    >
                        {loadingMore && <Loader2Icon className="w-3.5 h-3.5 animate-spin" />}
                        Load more
                    </button>
                </div>
            )}
        </div>
    );
}

function DeliveryRow({ delivery }: { delivery: WebhookDelivery }) {
    const [open, setOpen] = React.useState(false);
    const redeliver = useRedeliverDelivery();

    const onRedeliver = async (e: React.MouseEvent) => {
        e.stopPropagation();
        try {
            await redeliver.mutateAsync(delivery.id);
            toast.success("Queued for redelivery.");
        } catch (err) {
            toast.error((err as { message?: string })?.message ?? "Could not redeliver");
        }
    };

    const prettyPayload = React.useMemo(() => {
        try {
            return JSON.stringify(delivery.payload, null, 2);
        } catch {
            return String(delivery.payload);
        }
    }, [delivery.payload]);

    return (
        <div>
            <button
                type="button"
                onClick={() => setOpen((o) => !o)}
                className="w-full flex items-center gap-2 px-2.5 py-2 text-left hover:bg-slate-50/80 transition-colors"
            >
                <ChevronRightIcon className={cn("w-3.5 h-3.5 text-slate-300 shrink-0 transition-transform", open && "rotate-90")} />
                <code className="text-[11.5px] font-mono text-slate-700 truncate flex-1 min-w-0">{delivery.event_type}</code>
                <DeliveryStatusBadge status={delivery.status} />
                {typeof delivery.response_status === "number" && (
                    <span className={cn("text-[11px] font-mono tabular-nums", delivery.response_status >= 200 && delivery.response_status < 300 ? "text-emerald-600" : "text-rose-600")}>
                        {delivery.response_status}
                    </span>
                )}
                <span className="text-[10.5px] text-slate-400 tabular-nums shrink-0">
                    {delivery.attempt_count}/{delivery.max_attempts}
                </span>
                <span className="text-[10.5px] text-slate-400 shrink-0 hidden sm:inline">{formatRelative(delivery.created_at)}</span>
            </button>
            <AnimatePresence initial={false}>
                {open && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18 }}
                        className="overflow-hidden bg-slate-50/60"
                    >
                        <div className="px-3 py-2.5 space-y-2.5">
                            {delivery.error_reason && (
                                <div className="rounded-md border border-rose-200 bg-rose-50 px-2.5 py-1.5 text-[11px] text-rose-700 leading-relaxed">
                                    <span className="font-medium">Error:</span> {delivery.error_reason}
                                </div>
                            )}
                            {delivery.response_body_excerpt && (
                                <div>
                                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1">Response body</div>
                                    <pre className="rounded-md border border-slate-200 bg-white p-2 text-[10.5px] font-mono text-slate-600 whitespace-pre-wrap break-words max-h-32 overflow-y-auto">
                                        {delivery.response_body_excerpt}
                                    </pre>
                                </div>
                            )}
                            <div>
                                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-1">Payload</div>
                                <pre className="rounded-md border border-slate-200 bg-white p-2 text-[10.5px] font-mono text-slate-600 whitespace-pre-wrap break-words max-h-64 overflow-y-auto">
                                    {prettyPayload}
                                </pre>
                            </div>
                            <div className="flex items-center justify-between gap-2">
                                <span className="text-[10.5px] text-slate-400">
                                    Last attempt {formatRelative(delivery.last_attempt_at ?? delivery.created_at)}
                                </span>
                                <button
                                    type="button"
                                    onClick={onRedeliver}
                                    disabled={redeliver.isPending}
                                    className="h-7 px-2.5 rounded-md border border-slate-200 text-[11.5px] text-slate-700 hover:bg-white disabled:opacity-60 inline-flex items-center gap-1.5"
                                >
                                    {redeliver.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                                    Redeliver
                                </button>
                            </div>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

function SettingsTab({
    endpoint,
    catalog,
    onClose,
}: {
    endpoint: WebhookEndpoint;
    catalog: WebhookEventDescriptor[];
    onClose: () => void;
}) {
    const update = useUpdateWebhook();
    const del = useDeleteWebhook();
    const confirm = useConfirm();

    const [url, setUrl] = React.useState(endpoint.url);
    const [description, setDescription] = React.useState(endpoint.description);
    const [enabled, setEnabled] = React.useState(endpoint.enabled);
    const [events, setEvents] = React.useState<string[]>(endpoint.event_types);

    const dirty =
        url !== endpoint.url ||
        description !== endpoint.description ||
        enabled !== endpoint.enabled ||
        JSON.stringify(events) !== JSON.stringify(endpoint.event_types);

    const urlChanged = url.trim() !== endpoint.url;
    const urlValid = /^https:\/\/.+/i.test(url.trim());

    const save = async () => {
        if (!urlValid) {
            toast.error("Enter a valid endpoint URL");
            return;
        }
        try {
            await update.mutateAsync({
                id: endpoint.id,
                data: { url: url.trim(), description: description.trim(), event_types: events, enabled },
            });
            toast.success("Endpoint updated");
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not update the endpoint");
        }
    };

    const onDelete = () =>
        confirm.show(`Delete this endpoint? Deliveries to ${endpoint.url} stop immediately.`, async () => {
            await del.mutateAsync(endpoint.id);
            onClose();
        });

    return (
        <div className="p-5 space-y-5">
            <div>
                <Label>Endpoint URL</Label>
                <TextInput value={url} onChange={setUrl} placeholder="https://acme.com/webhooks/warmbly" className="w-full" />
                {urlChanged && (
                    <p className="mt-1 text-[11px] text-amber-700">
                        Changing the URL re-arms verification. The endpoint must reply 2xx to a fresh test event.
                    </p>
                )}
            </div>
            <div>
                <Label>Description</Label>
                <TextInput value={description} onChange={setDescription} placeholder="What this endpoint is for" className="w-full" />
            </div>
            <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                    <div className="text-[12.5px] font-medium text-slate-900">Enabled</div>
                    <div className="text-[11.5px] text-slate-500">When off, no deliveries are attempted.</div>
                </div>
                <Toggle on={enabled} onChange={setEnabled} />
            </div>
            <div>
                <Label>Events</Label>
                <EventPicker catalog={catalog} value={events} onChange={setEvents} />
            </div>

            <div className="flex items-center gap-2 pt-1">
                <button
                    type="button"
                    onClick={save}
                    disabled={!dirty || update.isPending}
                    className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 disabled:opacity-60 inline-flex items-center gap-1.5"
                >
                    {update.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckIcon className="w-3.5 h-3.5" />}
                    Save changes
                </button>
            </div>

            <div className="border-t border-slate-200 pt-4">
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium mb-2">Danger zone</div>
                <button
                    type="button"
                    onClick={onDelete}
                    className="h-8 px-3 rounded-md border border-rose-200 text-[12.5px] text-rose-600 hover:bg-rose-50 inline-flex items-center gap-1.5"
                >
                    <Trash2Icon className="w-3.5 h-3.5" /> Delete endpoint
                </button>
            </div>
        </div>
    );
}

/* ── throttle-drops strip ─────────────────────── */

function DropsStrip() {
    const drops = useWebhookDrops();
    const list = drops.data?.drops ?? [];
    if (list.length === 0) return null;
    return (
        <div className="mb-4 rounded-md border border-amber-200 bg-amber-50 p-3">
            <div className="flex items-center gap-1.5 text-[11.5px] font-medium text-amber-800">
                <AlertTriangleIcon className="w-3.5 h-3.5" /> Rate-limited events
            </div>
            <p className="text-[11px] text-amber-700 mt-1 leading-relaxed">
                Some high-volume events were throttled and not delivered. Subscribe to fewer firehose events, or expect gaps in
                these streams.
            </p>
            <div className="mt-2 space-y-1">
                {list.map((d) => (
                    <div key={`${d.event_type}-${d.day}`} className="flex items-center gap-2 text-[11px] text-amber-800">
                        <code className="font-mono">{d.event_type}</code>
                        <span className="text-amber-600">·</span>
                        <span>{d.dropped_windows} {d.dropped_windows === 1 ? "window" : "windows"} dropped</span>
                        <span className="text-amber-600">·</span>
                        <span>last {formatRelative(d.last_dropped_at)}</span>
                    </div>
                ))}
            </div>
        </div>
    );
}

/* ── page ─────────────────────── */

export default function WebhooksPage() {
    const canManage = usePermission("MANAGE_SETTINGS");
    const webhooks = useWebhooks();
    const [createOpen, setCreateOpen] = React.useState(false);
    const [openId, setOpenId] = React.useState<string | null>(null);

    if (!canManage) return <NoAccess feature="Webhooks" permissionLabel="Manage settings" />;

    const endpoints = webhooks.data?.endpoints ?? [];
    const catalog = webhooks.data?.event_types ?? [];
    const openEndpoint = endpoints.find((e) => e.id === openId) ?? null;

    return (
        <SectionShell
            title="Webhooks"
            description="Receive realtime HTTP callbacks when things happen in your workspace."
            actions={
                <button
                    onClick={() => setCreateOpen(true)}
                    className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5"
                >
                    <PlusIcon className="w-3.5 h-3.5" /> Add endpoint
                </button>
            }
        >
            <div className="px-4 py-5 md:px-8 md:py-6">
                <DropsStrip />

                {webhooks.isPending ? (
                    <div className="py-12 text-center text-[12px] text-slate-400">Loading endpoints…</div>
                ) : endpoints.length === 0 ? (
                    <EmptyBlock
                        title="No webhook endpoints yet"
                        body="Add an HTTPS endpoint and we'll POST a signed callback for the events you subscribe to."
                    />
                ) : (
                    <div className="space-y-2">
                        {endpoints.map((e) => (
                            <EndpointRow key={e.id} endpoint={e} onOpen={() => setOpenId(e.id)} />
                        ))}
                    </div>
                )}
            </div>

            {/* Own AnimatePresence so the modal's entrance isn't suppressed by the
                settings layout's <AnimatePresence initial={false}> (its context
                flows through the portal); also enables the exit animation. */}
            <AnimatePresence>
                {createOpen && <CreateModal key="create-webhook" catalog={catalog} onClose={() => setCreateOpen(false)} />}
            </AnimatePresence>
            {openEndpoint && (
                <EndpointDrawer endpoint={openEndpoint} catalog={catalog} onClose={() => setOpenId(null)} />
            )}
        </SectionShell>
    );
}
