// OAuth apps — a Settings section. Developers register third-party OAuth clients
// here (client id + one-time secret, redirect URIs, requested scopes) and review
// the apps the workspace's members have authorized. Every app is issued a client
// secret; PKCE is an optional extra layer the developer can add. The flow itself
// (consent + token exchange) lives on the standalone /oauth/authorize page + API.

import React from "react";
import { createPortal } from "react-dom";
import toast from "react-hot-toast";
import { AnimatePresence, motion } from "framer-motion";
import {
    ArrowLeftIcon,
    ArrowRightIcon,
    CheckCircle2Icon,
    CheckIcon,
    ChevronRightIcon,
    CopyIcon,
    ImageIcon,
    Loader2Icon,
    PencilIcon,
    PlusIcon,
    RefreshCwIcon,
    Trash2Icon,
    WebhookIcon,
    XIcon,
} from "lucide-react";

import { NoAccess } from "@/components/layout/NoAccess";
import { usePermission } from "@/hooks/usePermission";
import { EmptyBlock } from "@/components/layout/Page";
import { Label, SearchInput, TextInput } from "@/components/ui/field";
import { cn } from "@/lib/utils";
import { useConfirm } from "@/hooks/context/confirm";
import useAPIPermissions from "@/lib/api/hooks/app/api-keys/useAPIPermissions";
import type APIPermission from "@/lib/api/models/app/apikeys/APIPermission";
import {
    useOAuthApps,
    useCreateOAuthApp,
    useUpdateOAuthApp,
    useDeleteOAuthApp,
    useRotateOAuthAppSecret,
    useUploadOAuthAppLogo,
    useOAuthAppWebhookSecret,
    useRotateOAuthAppWebhookSecret,
    useOAuthAppWebhookEndpoints,
    useOAuthAppWebhookDeliveries,
} from "@/lib/api/hooks/app/oauth/useOAuthApps";
import { useWebhookEventCatalog } from "@/lib/api/hooks/app/webhooks/useWebhooks";
import listOAuthAppWebhookDeliveries from "@/lib/api/client/app/oauth/listOAuthAppWebhookDeliveries";
import { useAuthorizedApps, useRevokeAuthorizedApp } from "@/lib/api/hooks/app/oauth/useAuthorizedApps";
import type { OAuthApplication, OAuthApplicationWithSecret } from "@/lib/api/models/app/oauth/OAuthApp";
import type {
    WebhookDelivery,
    WebhookDeliveryStatus,
    WebhookEndpoint,
    WebhookEventDescriptor,
} from "@/lib/api/models/app/webhooks/Webhook";
import { SectionShell } from "../_components/SectionShell";

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

function hostOf(url: string): string {
    try {
        return new URL(url).host;
    } catch {
        return url;
    }
}

function CopyButton({ value, label }: { value: string; label?: string }) {
    const [copied, setCopied] = React.useState(false);
    return (
        <button
            type="button"
            onClick={async () => {
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

// ScopePicker is a checkbox grid over the API permissions, toggling bits in the
// scope bitmask. Reuses the same permission catalogue as API keys.
function ScopePicker({ value, onChange }: { value: number; onChange: (v: number) => void }) {
    const perms = useAPIPermissions();
    const grouped = React.useMemo(() => {
        const g: Record<string, APIPermission[]> = {};
        for (const p of perms.data?.permissions ?? []) {
            (g[p.category] ??= []).push(p);
        }
        return g;
    }, [perms.data]);
    const toggle = (bit: number) => onChange(value & bit ? value & ~bit : value | bit);
    return (
        <div className="space-y-3">
            {Object.entries(grouped).map(([cat, list]) => (
                <div key={cat}>
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 mb-1">{cat}</div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-1">
                        {list.map((p) => {
                            const on = (value & p.value) === p.value;
                            return (
                                <button
                                    key={p.name}
                                    type="button"
                                    onClick={() => toggle(p.value)}
                                    className={`flex items-start gap-2 rounded-md border px-2 py-1.5 text-left ${on ? "border-sky-400 bg-sky-50" : "border-slate-200 hover:bg-slate-50"}`}
                                >
                                    <span
                                        className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded ${on ? "bg-sky-600 text-white" : "border border-slate-300"}`}
                                    >
                                        {on && <CheckIcon className="w-3 h-3" />}
                                    </span>
                                    <span className="min-w-0">
                                        <span className="block text-[12px] font-medium text-slate-700">
                                            {p.name.toLowerCase()}
                                        </span>
                                        <span className="block text-[11px] text-slate-400 leading-tight">{p.description}</span>
                                    </span>
                                </button>
                            );
                        })}
                    </div>
                </div>
            ))}
        </div>
    );
}

// EventPicker — grouped by WebhookEventDescriptor.category with a search header
// and checkbox-square rows. Empty selection = "all events the granting org
// allows"; firehose events are flagged with an amber chip.
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
                    <>Subscribed to all events the granting org allows. New event types are included automatically (high-volume events excluded).</>
                ) : (
                    <>
                        Subscribed to <span className="font-medium">{value.length}</span>{" "}
                        {value.length === 1 ? "event" : "events"}. Leave none selected to receive all the events each org allows.
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

const TILE_COLORS = ["bg-sky-600", "bg-indigo-600", "bg-emerald-600", "bg-rose-600", "bg-amber-600", "bg-fuchsia-600"];

// AppLogo renders an app's uploaded logo, or a colored letter tile as a fallback.
function AppLogo({ name, url, size = "md" }: { name: string; url?: string | null; size?: "sm" | "md" | "lg" }) {
    const dim =
        size === "lg"
            ? "w-16 h-16 text-[22px] rounded-2xl"
            : size === "sm"
              ? "w-8 h-8 text-[12px] rounded-md"
              : "w-9 h-9 text-[13px] rounded-lg";
    if (url) return <img src={url} alt={name} className={cn(dim, "object-cover border border-slate-200 shrink-0")} />;
    const letter = (name.trim()[0] ?? "?").toUpperCase();
    const color = TILE_COLORS[letter.charCodeAt(0) % TILE_COLORS.length];
    return <div className={cn(dim, color, "flex items-center justify-center text-white font-semibold shrink-0")}>{letter}</div>;
}

const WIZARD_STEPS = ["Basics", "Branding", "Redirects", "Webhook domains", "Webhooks", "Scopes"] as const;

// Stepper — the horizontal progress indicator at the top of the register wizard.
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

// RegisterModal — a multi-step onboarding wizard for creating an OAuth app:
// Basics -> Branding (logo) -> Redirects -> Scopes -> reveal credentials. The
// modal animates in, steps slide, and a stepper shows progress.
function RegisterModal({ onClose }: { onClose: () => void }) {
    const create = useCreateOAuthApp();
    const uploadLogo = useUploadOAuthAppLogo();
    const catalog = useWebhookEventCatalog();
    const fileRef = React.useRef<HTMLInputElement>(null);

    const [step, setStep] = React.useState(0);
    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [website, setWebsite] = React.useState("");
    const [logoUrl, setLogoUrl] = React.useState("");
    const [redirects, setRedirects] = React.useState("");
    const [webhookDomains, setWebhookDomains] = React.useState("");
    const [webhookUrl, setWebhookUrl] = React.useState("");
    const [webhookEvents, setWebhookEvents] = React.useState<string[]>([]);
    const [scopes, setScopes] = React.useState(0);
    const [created, setCreated] = React.useState<OAuthApplicationWithSecret | null>(null);

    const redirectList = redirects.split("\n").map((s) => s.trim()).filter(Boolean);
    const webhookDomainList = webhookDomains.split("\n").map((s) => s.trim()).filter(Boolean);

    const webhookUrlValid = webhookUrl.trim() === "" || /^https:\/\/.+/i.test(webhookUrl.trim());

    const stepValid = (i: number): boolean => {
        if (i === 0) return name.trim().length > 0;
        if (i === 2) return redirectList.length > 0;
        if (i === 4) return webhookUrlValid; // webhook step optional, but the URL must be https if given
        if (i === 5) return scopes !== 0;
        return true; // branding (logo) and webhook domains are optional
    };

    const goNext = () => {
        if (!stepValid(step)) {
            toast.error(
                step === 0
                    ? "Give the app a name"
                    : step === 2
                      ? "Add at least one redirect URI"
                      : step === 4
                        ? "The webhook URL must start with https://"
                        : "Select at least one scope",
            );
            return;
        }
        setStep((s) => Math.min(s + 1, WIZARD_STEPS.length - 1));
    };
    const goBack = () => setStep((s) => Math.max(s - 1, 0));

    const onPickLogo = async (e: React.ChangeEvent<HTMLInputElement>) => {
        const file = e.target.files?.[0];
        e.target.value = "";
        if (!file) return;
        try {
            const { logo_url } = await uploadLogo.mutateAsync(file);
            setLogoUrl(logo_url);
        } catch (err) {
            toast.error((err as { message?: string })?.message ?? "Could not upload the logo");
        }
    };

    const submit = async () => {
        if (!stepValid(0) || !stepValid(2) || !stepValid(4) || !stepValid(5)) {
            toast.error("Fill in the required fields");
            return;
        }
        try {
            const app = await create.mutateAsync({
                name: name.trim(),
                description: description.trim(),
                website_url: website.trim(),
                logo_url: logoUrl || undefined,
                redirect_uris: redirectList,
                allowed_webhook_domains: webhookDomainList,
                webhook_url: webhookUrl.trim() || undefined,
                webhook_events: webhookUrl.trim() ? webhookEvents : undefined,
                scopes,
            });
            setCreated(app);
            toast.success("App registered");
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not register the app");
        }
    };

    // Rendered through a portal to document.body so the modal mounts as a clean
    // top-level subtree (outside the Settings content's AnimatePresence) and its
    // entrance animation always plays.
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
                        {created ? "App created" : "Register an OAuth app"}
                    </span>
                    <button onClick={onClose} className="ml-auto h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100">
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>

                {created ? (
                    <RevealStep app={created} onDone={onClose} />
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
                                                <Label>Name</Label>
                                                <TextInput value={name} onChange={setName} placeholder="Acme Integration" className="w-full" />
                                            </div>
                                            <div>
                                                <Label>Description</Label>
                                                <TextInput value={description} onChange={setDescription} placeholder="What the app does" className="w-full" />
                                            </div>
                                            <div>
                                                <Label>Website</Label>
                                                <TextInput value={website} onChange={setWebsite} placeholder="https://acme.com" className="w-full" />
                                            </div>
                                        </>
                                    )}
                                    {step === 1 && (
                                        <>
                                            <p className="text-[12px] text-slate-500 leading-relaxed">
                                                Add a logo so people recognize your app on the consent screen when they connect it.
                                            </p>
                                            <div className="flex items-center gap-3">
                                                <AppLogo name={name || "?"} url={logoUrl} size="lg" />
                                                <div>
                                                    <input ref={fileRef} type="file" accept="image/png,image/jpeg" className="hidden" onChange={onPickLogo} />
                                                    <div className="flex items-center gap-2">
                                                        <button
                                                            type="button"
                                                            onClick={() => fileRef.current?.click()}
                                                            disabled={uploadLogo.isPending}
                                                            className="h-8 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:bg-slate-50 inline-flex items-center gap-1.5 disabled:opacity-60"
                                                        >
                                                            {uploadLogo.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <ImageIcon className="w-3.5 h-3.5" />}
                                                            {logoUrl ? "Replace logo" : "Upload logo"}
                                                        </button>
                                                        {logoUrl && (
                                                            <button type="button" onClick={() => setLogoUrl("")} className="text-[11.5px] text-slate-400 hover:text-rose-600">
                                                                Remove
                                                            </button>
                                                        )}
                                                    </div>
                                                    <p className="mt-1.5 text-[10.5px] text-slate-400">PNG or JPG, up to 2MB. Optional.</p>
                                                </div>
                                            </div>
                                        </>
                                    )}
                                    {step === 2 && (
                                        <div>
                                            <Label>Redirect URIs</Label>
                                            <textarea
                                                value={redirects}
                                                onChange={(e) => setRedirects(e.target.value)}
                                                placeholder={"https://acme.com/oauth/callback"}
                                                rows={3}
                                                className="w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[12px] font-mono text-slate-900 placeholder:text-slate-400 outline-none resize-y focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                            />
                                            <p className="mt-1 text-[11px] text-slate-400">One per line. Must be HTTPS (or a loopback URL), matched exactly.</p>
                                        </div>
                                    )}
                                    {step === 3 && (
                                        <div>
                                            <Label>Webhook domains</Label>
                                            <textarea
                                                value={webhookDomains}
                                                onChange={(e) => setWebhookDomains(e.target.value)}
                                                placeholder={".acme.com\nhooks.partner.com"}
                                                rows={3}
                                                className="w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[12px] font-mono text-slate-900 placeholder:text-slate-400 outline-none resize-y focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                                            />
                                            <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                                                Webhooks this app registers must point at these domains. Use a leading dot for subdomains
                                                (.acme.com matches hooks.acme.com); a bare domain (acme.com) is an exact match. Leave empty to
                                                forbid this app from registering webhooks.
                                            </p>
                                        </div>
                                    )}
                                    {step === 4 && (
                                        <>
                                            <p className="text-[12px] text-slate-500 leading-relaxed">
                                                Optionally receive Warmbly events over webhooks. Each org that authorizes the app gets its own
                                                signed delivery stream, scoped to what that org granted. You can skip this and add it later.
                                            </p>
                                            <div>
                                                <Label>Webhook URL</Label>
                                                <TextInput
                                                    value={webhookUrl}
                                                    onChange={setWebhookUrl}
                                                    placeholder="https://hooks.acme.com/warmbly"
                                                    className="w-full"
                                                />
                                                <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                                                    Must be https and its host must fall within the allowed webhook domains above. Leave empty to
                                                    skip webhooks for now.
                                                </p>
                                                {!webhookUrlValid && (
                                                    <p className="mt-1 text-[11px] text-rose-600">The webhook URL must start with https://</p>
                                                )}
                                            </div>
                                            {webhookUrl.trim() !== "" && (
                                                <div>
                                                    <Label>Events</Label>
                                                    {catalog.isPending ? (
                                                        <div className="py-6 text-center text-[11.5px] text-slate-400">Loading events…</div>
                                                    ) : (
                                                        <EventPicker
                                                            catalog={catalog.data?.event_types ?? []}
                                                            value={webhookEvents}
                                                            onChange={setWebhookEvents}
                                                        />
                                                    )}
                                                </div>
                                            )}
                                        </>
                                    )}
                                    {step === 5 && (
                                        <div>
                                            <Label>Scopes</Label>
                                            <ScopePicker value={scopes} onChange={setScopes} />
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
                            {step < WIZARD_STEPS.length - 1 ? (
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
                                    Create app
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

// RevealStep shows the one-time client credentials after the app is created.
function RevealStep({ app, onDone }: { app: OAuthApplicationWithSecret; onDone: () => void }) {
    return (
        <div className="flex-1 overflow-y-auto p-4 space-y-3">
            <div className="flex items-center gap-2 text-emerald-600">
                <CheckCircle2Icon className="w-4 h-4" />
                <span className="text-[12.5px] font-medium">{app.name} is ready</span>
            </div>
            <p className="text-[12px] text-slate-500 leading-relaxed">
                Save the client secret now. For security it is shown only once and cannot be retrieved later (you can rotate it if you lose it).
            </p>
            <div>
                <Label>Client ID</Label>
                <div className="flex items-center gap-1.5">
                    <code className="flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-slate-700">
                        {app.client_id}
                    </code>
                    <CopyButton value={app.client_id} />
                </div>
            </div>
            <div>
                <Label>Client secret</Label>
                <div className="flex items-center gap-1.5">
                    <code className="flex-1 truncate rounded-md border border-amber-200 bg-amber-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-amber-800">
                        {app.client_secret}
                    </code>
                    <CopyButton value={app.client_secret ?? ""} />
                </div>
            </div>
            <div className="pt-1">
                <button onClick={onDone} className="h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700">
                    Done
                </button>
            </div>
        </div>
    );
}

// AppWebhookChip — an at-a-glance chip when the app has a webhook URL set,
// showing the live install count (per-org endpoints it materialized).
function AppWebhookChip({ app }: { app: OAuthApplication }) {
    const endpoints = useOAuthAppWebhookEndpoints(app.id, true);
    const installs = endpoints.data?.endpoints.length ?? 0;
    return (
        <span
            title={app.webhook_url}
            className="inline-flex items-center gap-1 rounded border border-sky-200 bg-sky-50 px-1.5 py-0.5 text-[10.5px] font-medium text-sky-700"
        >
            <WebhookIcon className="w-3 h-3" />
            {endpoints.isPending
                ? hostOf(app.webhook_url)
                : `${installs} ${installs === 1 ? "install" : "installs"}`}
        </span>
    );
}

function AppRow({ app }: { app: OAuthApplication }) {
    const del = useDeleteOAuthApp();
    const rotate = useRotateOAuthAppSecret();
    const confirm = useConfirm();
    const [secret, setSecret] = React.useState<string | null>(null);
    const [editOpen, setEditOpen] = React.useState(false);

    const onRotate = () =>
        confirm.show("Rotate this app's client secret? The current secret stops working immediately.", async () => {
            const res = await rotate.mutateAsync(app.id);
            setSecret(res.client_secret);
        });
    const onDelete = () =>
        confirm.show(`Delete "${app.name}"? Every token issued to it is revoked.`, async () => {
            await del.mutateAsync(app.id);
        });

    const webhookDomains = app.allowed_webhook_domains ?? [];

    return (
        <div className="rounded-lg border border-slate-200 p-3">
            <div className="flex items-start gap-3">
                <AppLogo name={app.name} url={app.logo_url} size="md" />
                <div className="min-w-0 flex-1">
                    <div className="text-[13px] font-semibold text-slate-800 truncate">{app.name}</div>
                    {app.description && <div className="text-[11.5px] text-slate-400 truncate">{app.description}</div>}
                    <div className="mt-1.5 flex items-center gap-1.5 flex-wrap">
                        <code className="truncate rounded border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[11px] font-mono text-slate-600">
                            {app.client_id}
                        </code>
                        <CopyButton value={app.client_id} label="ID" />
                        {app.webhook_url && <AppWebhookChip app={app} />}
                    </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                    <button
                        onClick={() => setEditOpen(true)}
                        title="Edit app"
                        className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                    >
                        <PencilIcon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        onClick={onRotate}
                        title="Rotate secret"
                        className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                    >
                        <RefreshCwIcon className="w-3.5 h-3.5" />
                    </button>
                    <button
                        onClick={onDelete}
                        title="Delete app"
                        className="h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-rose-50 hover:text-rose-600"
                    >
                        <Trash2Icon className="w-3.5 h-3.5" />
                    </button>
                </div>
            </div>
            <div className="mt-2">
                <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 mb-1">Redirect URIs</div>
                <div className="flex flex-wrap gap-1">
                    {app.redirect_uris.map((u) => (
                        <span key={u} className="rounded bg-slate-100 px-1.5 py-0.5 text-[10.5px] font-mono text-slate-500">
                            {u}
                        </span>
                    ))}
                </div>
            </div>
            <div className="mt-2">
                <div className="text-[10px] uppercase tracking-[0.12em] text-slate-400 mb-1">Webhook domains</div>
                {webhookDomains.length === 0 ? (
                    <span className="text-[10.5px] text-slate-400">No webhook domains. This app can't register webhooks.</span>
                ) : (
                    <div className="flex flex-wrap gap-1">
                        {webhookDomains.map((d) => (
                            <span key={d} className="rounded bg-slate-100 px-1.5 py-0.5 text-[10.5px] font-mono text-slate-500">
                                {d}
                            </span>
                        ))}
                    </div>
                )}
            </div>
            {secret && (
                <div className="mt-2 rounded-md border border-amber-200 bg-amber-50 p-2">
                    <div className="text-[10.5px] uppercase tracking-[0.12em] text-amber-700 mb-1">New client secret (shown once)</div>
                    <div className="flex items-center gap-1.5">
                        <code className="flex-1 truncate text-[11.5px] font-mono text-amber-800">{secret}</code>
                        <CopyButton value={secret} />
                    </div>
                </div>
            )}
            <AnimatePresence>
                {editOpen && <EditModal key="edit-oauth-app" app={app} onClose={() => setEditOpen(false)} />}
            </AnimatePresence>
        </div>
    );
}

// AppWebhookSecretRow — reveal + rotate the app-level webhook signing secret.
function AppWebhookSecretRow({ app }: { app: OAuthApplication }) {
    const [reveal, setReveal] = React.useState(false);
    const secret = useOAuthAppWebhookSecret(app.id, reveal);
    const rotate = useRotateOAuthAppWebhookSecret();
    const confirm = useConfirm();
    const [rotated, setRotated] = React.useState<string | null>(null);

    const onRotate = () =>
        confirm.show("Rotate this app's webhook signing secret? The current secret stops working immediately.", async () => {
            const res = await rotate.mutateAsync(app.id);
            setRotated(res.webhook_secret);
            setReveal(true);
        });

    return (
        <div className="space-y-2.5">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Signing secret</div>
            <p className="text-[11.5px] text-slate-500 leading-relaxed">
                Verify the HMAC signature on every delivery with this secret. Shared across all installs of this app.
            </p>
            <div className="flex items-center gap-1.5">
                {reveal && secret.data ? (
                    <code className="flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-slate-700">
                        {secret.data.webhook_secret}
                    </code>
                ) : (
                    <code className="flex-1 truncate rounded-md border border-slate-200 bg-slate-50 px-2 h-7 inline-flex items-center text-[11.5px] font-mono text-slate-400">
                        {reveal && secret.isPending ? "Loading…" : "••••••••••••••••••••••••"}
                    </code>
                )}
                {reveal && secret.data ? (
                    <CopyButton value={secret.data.webhook_secret} />
                ) : (
                    <button
                        type="button"
                        onClick={() => setReveal(true)}
                        className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 h-7 text-[11.5px] text-slate-600 hover:bg-slate-50"
                    >
                        Reveal
                    </button>
                )}
                <button
                    type="button"
                    onClick={onRotate}
                    disabled={rotate.isPending}
                    className="inline-flex items-center gap-1 rounded-md border border-slate-200 px-2 h-7 text-[11.5px] text-slate-600 hover:bg-slate-50 disabled:opacity-60"
                >
                    {rotate.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <RefreshCwIcon className="w-3.5 h-3.5" />}
                    Rotate
                </button>
            </div>
            {rotated && (
                <div className="rounded-md border border-amber-200 bg-amber-50 p-2">
                    <div className="text-[10.5px] uppercase tracking-[0.12em] text-amber-700 mb-1">New signing secret (shown once)</div>
                    <div className="flex items-center gap-1.5">
                        <code className="flex-1 truncate text-[11.5px] font-mono text-amber-800">{rotated}</code>
                        <CopyButton value={rotated} />
                    </div>
                </div>
            )}
        </div>
    );
}

// AppWebhookInstalls — the per-org endpoints the app materialized, with health.
function AppWebhookInstalls({ app }: { app: OAuthApplication }) {
    const endpoints = useOAuthAppWebhookEndpoints(app.id, true);
    const list = endpoints.data?.endpoints ?? [];

    return (
        <div className="space-y-2">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                Installations{list.length > 0 ? ` (${list.length})` : ""}
            </div>
            {endpoints.isPending ? (
                <div className="py-4 text-center text-[11.5px] text-slate-400">Loading installs…</div>
            ) : list.length === 0 ? (
                <p className="text-[11.5px] text-slate-400">No orgs have installed this app's webhook yet.</p>
            ) : (
                <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                    {list.map((e) => (
                        <InstallRow key={e.id} endpoint={e} />
                    ))}
                </div>
            )}
        </div>
    );
}

function InstallRow({ endpoint }: { endpoint: WebhookEndpoint }) {
    const failing = endpoint.consecutive_failures > 0;
    return (
        <div className="flex items-center gap-2 px-2.5 py-2">
            <div className="min-w-0 flex-1">
                <div className="flex items-center gap-1.5 flex-wrap">
                    <code className="text-[11.5px] font-mono text-slate-700 truncate">{endpoint.organization_id}</code>
                    {!endpoint.enabled ? (
                        <span className="inline-flex items-center rounded-sm bg-slate-100 border border-slate-200 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-slate-500">
                            Disabled
                        </span>
                    ) : endpoint.verified_at ? (
                        <span className="inline-flex items-center rounded-sm bg-emerald-50 border border-emerald-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-emerald-700">
                            Verified
                        </span>
                    ) : (
                        <span className="inline-flex items-center rounded-sm bg-amber-50 border border-amber-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-amber-700">
                            Pending
                        </span>
                    )}
                    {failing && (
                        <span className="inline-flex items-center rounded-sm bg-rose-50 border border-rose-100 px-1.5 py-0.5 text-[10px] uppercase tracking-[0.06em] font-semibold text-rose-700">
                            {endpoint.consecutive_failures} failing
                        </span>
                    )}
                </div>
                <div className="mt-0.5 text-[10.5px] text-slate-400">
                    Last success {formatRelative(endpoint.last_success_at)}
                </div>
            </div>
        </div>
    );
}

const APP_DELIVERY_STATUSES: WebhookDeliveryStatus[] = ["pending", "in_flight", "delivered", "failed", "abandoned"];

// AppWebhookDeliveries — the cross-org delivery log (read-only, no redeliver).
function AppWebhookDeliveries({ app, catalog }: { app: OAuthApplication; catalog: WebhookEventDescriptor[] }) {
    const [status, setStatus] = React.useState<WebhookDeliveryStatus | "">("");
    const [eventType, setEventType] = React.useState("");

    const first = useOAuthAppWebhookDeliveries(app.id, { status, eventType, limit: 25 });
    const [extra, setExtra] = React.useState<WebhookDelivery[]>([]);
    const [cursor, setCursor] = React.useState<string | null>(null);
    const [hasMore, setHasMore] = React.useState(false);
    const [loadingMore, setLoadingMore] = React.useState(false);

    React.useEffect(() => {
        setExtra([]);
        setCursor(first.data?.pagination.next_cursor ?? null);
        setHasMore(first.data?.pagination.has_more ?? false);
    }, [first.data, status, eventType]);

    const loadMore = async () => {
        if (!cursor) return;
        setLoadingMore(true);
        try {
            const res = await listOAuthAppWebhookDeliveries(app.id, { status, eventType, cursor, limit: 25 });
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
        <div className="space-y-2.5">
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">Recent deliveries</div>
            <div className="flex items-center gap-2 flex-wrap">
                <select
                    value={status}
                    onChange={(e) => setStatus(e.target.value as WebhookDeliveryStatus | "")}
                    className="h-7 rounded-md border border-slate-200 bg-white px-2 text-[12px] text-slate-700 outline-none focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                >
                    <option value="">All statuses</option>
                    {APP_DELIVERY_STATUSES.map((s) => (
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
                <div className="py-8 text-center text-[12px] text-slate-400">Loading deliveries…</div>
            ) : rows.length === 0 ? (
                <p className="text-[11.5px] text-slate-400 py-2">No deliveries yet. Once events fire, every attempt shows here.</p>
            ) : (
                <div className="rounded-md border border-slate-200 divide-y divide-slate-100">
                    {rows.map((d) => (
                        <AppDeliveryRow key={d.id} delivery={d} />
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

// AppDeliveryRow — one delivery attempt, expandable to payload + error (read-only).
function AppDeliveryRow({ delivery }: { delivery: WebhookDelivery }) {
    const [open, setOpen] = React.useState(false);

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
                    <span
                        className={cn(
                            "text-[11px] font-mono tabular-nums",
                            delivery.response_status >= 200 && delivery.response_status < 300 ? "text-emerald-600" : "text-rose-600",
                        )}
                    >
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
                            <span className="block text-[10.5px] text-slate-400">
                                Last attempt {formatRelative(delivery.last_attempt_at ?? delivery.created_at)}
                            </span>
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}

// EditModal — a simple form modal for an existing app, styled to match the
// create wizard. Edits name/description/website/redirects/webhook domains/
// webhook URL + events/scopes via useUpdateOAuthApp, and surfaces the app's
// webhook signing secret, installs, and delivery log when a webhook URL is set.
function EditModal({ app, onClose }: { app: OAuthApplication; onClose: () => void }) {
    const update = useUpdateOAuthApp();
    const catalog = useWebhookEventCatalog();

    const [name, setName] = React.useState(app.name);
    const [description, setDescription] = React.useState(app.description);
    const [website, setWebsite] = React.useState(app.website_url);
    const [redirects, setRedirects] = React.useState(app.redirect_uris.join("\n"));
    const [webhookDomains, setWebhookDomains] = React.useState((app.allowed_webhook_domains ?? []).join("\n"));
    const [webhookUrl, setWebhookUrl] = React.useState(app.webhook_url ?? "");
    const [webhookEvents, setWebhookEvents] = React.useState<string[]>(app.webhook_events ?? []);
    const [scopes, setScopes] = React.useState(app.scopes);

    const redirectList = redirects.split("\n").map((s) => s.trim()).filter(Boolean);
    const webhookDomainList = webhookDomains.split("\n").map((s) => s.trim()).filter(Boolean);

    const webhookUrlValid = webhookUrl.trim() === "" || /^https:\/\/.+/i.test(webhookUrl.trim());

    const save = async () => {
        if (name.trim().length === 0) {
            toast.error("Give the app a name");
            return;
        }
        if (redirectList.length === 0) {
            toast.error("Add at least one redirect URI");
            return;
        }
        if (!webhookUrlValid) {
            toast.error("The webhook URL must start with https://");
            return;
        }
        if (scopes === 0) {
            toast.error("Select at least one scope");
            return;
        }
        try {
            await update.mutateAsync({
                id: app.id,
                data: {
                    name: name.trim(),
                    description: description.trim(),
                    website_url: website.trim(),
                    logo_url: app.logo_url || undefined,
                    redirect_uris: redirectList,
                    allowed_webhook_domains: webhookDomainList,
                    webhook_url: webhookUrl.trim() || undefined,
                    webhook_events: webhookUrl.trim() ? webhookEvents : undefined,
                    scopes,
                },
            });
            toast.success("App updated");
            onClose();
        } catch (e) {
            toast.error((e as { message?: string })?.message ?? "Could not update the app");
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
                    <span className="text-[12.5px] font-medium text-slate-900">Edit {app.name}</span>
                    <button onClick={onClose} className="ml-auto h-7 w-7 inline-flex items-center justify-center rounded-md text-slate-400 hover:bg-slate-100">
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>
                <div className="flex-1 overflow-y-auto p-4 space-y-3">
                    <div>
                        <Label>Name</Label>
                        <TextInput value={name} onChange={setName} placeholder="Acme Integration" className="w-full" />
                    </div>
                    <div>
                        <Label>Description</Label>
                        <TextInput value={description} onChange={setDescription} placeholder="What the app does" className="w-full" />
                    </div>
                    <div>
                        <Label>Website</Label>
                        <TextInput value={website} onChange={setWebsite} placeholder="https://acme.com" className="w-full" />
                    </div>
                    <div>
                        <Label>Redirect URIs</Label>
                        <textarea
                            value={redirects}
                            onChange={(e) => setRedirects(e.target.value)}
                            placeholder={"https://acme.com/oauth/callback"}
                            rows={3}
                            className="w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[12px] font-mono text-slate-900 placeholder:text-slate-400 outline-none resize-y focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                        <p className="mt-1 text-[11px] text-slate-400">One per line. Must be HTTPS (or a loopback URL), matched exactly.</p>
                    </div>
                    <div>
                        <Label>Webhook domains</Label>
                        <textarea
                            value={webhookDomains}
                            onChange={(e) => setWebhookDomains(e.target.value)}
                            placeholder={".acme.com\nhooks.partner.com"}
                            rows={3}
                            className="w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[12px] font-mono text-slate-900 placeholder:text-slate-400 outline-none resize-y focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                        <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                            Webhooks this app registers must point at these domains. Use a leading dot for subdomains (.acme.com
                            matches hooks.acme.com); a bare domain (acme.com) is an exact match. Leave empty to forbid this app from
                            registering webhooks.
                        </p>
                    </div>
                    <div>
                        <Label>Webhook URL</Label>
                        <TextInput value={webhookUrl} onChange={setWebhookUrl} placeholder="https://hooks.acme.com/warmbly" className="w-full" />
                        <p className="mt-1 text-[11px] text-slate-400 leading-relaxed">
                            Must be https and its host must fall within the allowed webhook domains above. Leave empty to disable webhooks.
                        </p>
                        {!webhookUrlValid && (
                            <p className="mt-1 text-[11px] text-rose-600">The webhook URL must start with https://</p>
                        )}
                    </div>
                    {webhookUrl.trim() !== "" && (
                        <div>
                            <Label>Events</Label>
                            {catalog.isPending ? (
                                <div className="py-6 text-center text-[11.5px] text-slate-400">Loading events…</div>
                            ) : (
                                <EventPicker
                                    catalog={catalog.data?.event_types ?? []}
                                    value={webhookEvents}
                                    onChange={setWebhookEvents}
                                />
                            )}
                        </div>
                    )}
                    <div>
                        <Label>Scopes</Label>
                        <ScopePicker value={scopes} onChange={setScopes} />
                    </div>

                    {app.webhook_url && (
                        <div className="border-t border-slate-200 pt-4 space-y-5">
                            <AppWebhookSecretRow app={app} />
                            <AppWebhookInstalls app={app} />
                            <AppWebhookDeliveries app={app} catalog={catalog.data?.event_types ?? []} />
                        </div>
                    )}
                </div>
                <div className="px-4 py-2.5 border-t border-slate-200 flex items-center gap-2 shrink-0">
                    <button onClick={onClose} className="h-8 px-3 rounded-md border border-slate-200 text-[12.5px] text-slate-600 hover:bg-slate-50">
                        Cancel
                    </button>
                    <button
                        onClick={save}
                        disabled={update.isPending}
                        className="ml-auto h-8 px-3 rounded-md bg-sky-600 text-white text-[12.5px] font-medium hover:bg-sky-700 disabled:opacity-60 inline-flex items-center gap-1.5"
                    >
                        {update.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CheckIcon className="w-3.5 h-3.5" />}
                        Save changes
                    </button>
                </div>
            </motion.div>
        </div>,
        document.body,
    );
}

function AuthorizedTab() {
    const authorized = useAuthorizedApps();
    const revoke = useRevokeAuthorizedApp();
    const confirm = useConfirm();
    const apps = authorized.data?.authorized_apps ?? [];

    if (apps.length === 0) {
        return <EmptyBlock title="No authorized apps" body="Apps your workspace connects to via OAuth will appear here." />;
    }
    return (
        <div className="space-y-2">
            {apps.map((a) => (
                <div key={a.application_id} className="flex items-center gap-3 rounded-lg border border-slate-200 p-3">
                    <AppLogo name={a.name} url={a.logo_url} size="md" />
                    <div className="min-w-0 flex-1">
                        <div className="text-[13px] font-semibold text-slate-800 truncate">{a.name}</div>
                        <div className="text-[11.5px] text-slate-400">
                            Authorized {new Date(a.authorized_at).toLocaleDateString()}
                        </div>
                    </div>
                    <button
                        onClick={() =>
                            confirm.show(`Revoke "${a.name}"? Its tokens stop working immediately.`, async () => {
                                await revoke.mutateAsync(a.application_id);
                            })
                        }
                        className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-rose-50 hover:text-rose-600 hover:border-rose-200"
                    >
                        Revoke
                    </button>
                </div>
            ))}
        </div>
    );
}

export default function OAuthAppsPage() {
    const canManage = usePermission("MANAGE_API_KEYS");
    const apps = useOAuthApps();
    const [tab, setTab] = React.useState<"apps" | "authorized">("apps");
    const [createOpen, setCreateOpen] = React.useState(false);

    if (!canManage) return <NoAccess feature="OAuth apps" permissionLabel="Manage API keys" />;

    const list = apps.data?.applications ?? [];

    return (
        <SectionShell
            title="OAuth apps"
            description="Apps that connect to Warmbly on a user's behalf via OAuth2."
            actions={
                tab === "apps" ? (
                    <button
                        onClick={() => setCreateOpen(true)}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5"
                    >
                        <PlusIcon className="w-3.5 h-3.5" /> Register app
                    </button>
                ) : null
            }
        >
            <div className="px-4 py-5 md:px-8 md:py-6">
                <div className="mb-3 flex items-center gap-1 border-b border-slate-200">
                    {(
                        [
                            ["apps", "Your apps"],
                            ["authorized", "Authorized apps"],
                        ] as const
                    ).map(([k, label]) => (
                        <button
                            key={k}
                            onClick={() => setTab(k)}
                            className={`relative h-9 px-2.5 text-[12.5px] transition-colors ${tab === k ? "text-slate-900 font-medium" : "text-slate-500 hover:text-slate-700"}`}
                        >
                            {label}
                            {tab === k && (
                                <motion.span
                                    layoutId="oauth-apps-tab"
                                    className="absolute inset-x-1 -bottom-px h-0.5 rounded bg-sky-600"
                                    transition={{ type: "spring", stiffness: 520, damping: 40 }}
                                />
                            )}
                        </button>
                    ))}
                </div>

                <AnimatePresence mode="wait" initial={false}>
                    <motion.div
                        key={tab}
                        initial={{ opacity: 0, x: 8 }}
                        animate={{ opacity: 1, x: 0 }}
                        exit={{ opacity: 0, x: -8 }}
                        transition={{ duration: 0.15, ease: "easeOut" }}
                    >
                        {tab === "apps" ? (
                            list.length === 0 ? (
                                <EmptyBlock
                                    title="No OAuth apps yet"
                                    body="Register an app to let it request scoped access to Warmbly accounts via OAuth2."
                                />
                            ) : (
                                <div className="space-y-2">
                                    {list.map((app) => (
                                        <AppRow key={app.id} app={app} />
                                    ))}
                                </div>
                            )
                        ) : (
                            <AuthorizedTab />
                        )}
                    </motion.div>
                </AnimatePresence>
            </div>

            <AnimatePresence>
                {createOpen && <RegisterModal key="register-oauth-app" onClose={() => setCreateOpen(false)} />}
            </AnimatePresence>
        </SectionShell>
    );
}
