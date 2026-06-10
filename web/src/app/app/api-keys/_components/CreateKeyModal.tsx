// Create-API-key flow.
//
// Two screens:
//   1. Configure  — name, description, permissions (preset / custom),
//                   rate limit, expiration, IP allowlist.
//   2. Reveal     — show the freshly-minted secret once; copying it is
//                   the only way to recover it.
//
// Stripe-style: opinionated defaults (read-only preset, 60 r/m, no
// expiration), with advanced options behind a disclosure.

import React from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
    CheckIcon,
    CopyIcon,
    KeyIcon,
    Loader2Icon,
    LockIcon,
    MinusIcon,
    PlusIcon,
    ShieldCheckIcon,
    ShieldIcon,
    SlidersIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import useCreateAPIKey from "@/lib/api/hooks/app/api-keys/useCreateAPIKey";
import useAPIPermissions from "@/lib/api/hooks/app/api-keys/useAPIPermissions";
import type APIPermission from "@/lib/api/models/app/apikeys/APIPermission";
import type { APIKeyWithSecret } from "@/lib/api/models/app/apikeys/APIKey";

type Step = "configure" | "reveal";
type Preset = "read_only" | "full_access" | "custom";

export default function CreateKeyModal({ open, onClose }: { open: boolean; onClose: () => void }) {
    const [step, setStep] = React.useState<Step>("configure");
    const [created, setCreated] = React.useState<APIKeyWithSecret | null>(null);

    React.useEffect(() => {
        if (!open) {
            // Defer reset so the closing animation doesn't show empty state.
            const t = setTimeout(() => {
                setStep("configure");
                setCreated(null);
            }, 200);
            return () => clearTimeout(t);
        }
    }, [open]);

    return (
        <AnimatePresence>
            {open && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.15 }}
                        className="fixed inset-0 z-40 bg-black/40"
                        onClick={step === "reveal" ? undefined : onClose}
                    />
                    <motion.div
                        initial={{ opacity: 0, y: 8, scale: 0.99 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 8, scale: 0.99 }}
                        transition={{ duration: 0.18 }}
                        className="fixed left-1/2 top-[10dvh] z-50 -translate-x-1/2 w-[560px] max-w-[92vw] bg-white rounded-lg border border-slate-200 shadow-[0_24px_60px_-12px_rgba(15,23,42,0.25)] overflow-hidden"
                    >
                        {step === "configure" ? (
                            <ConfigureStep
                                onCancel={onClose}
                                onCreated={(k) => {
                                    setCreated(k);
                                    setStep("reveal");
                                }}
                            />
                        ) : created ? (
                            <RevealStep apiKey={created} onClose={onClose} />
                        ) : null}
                    </motion.div>
                </>
            )}
        </AnimatePresence>
    );
}

function ConfigureStep({
    onCancel,
    onCreated,
}: {
    onCancel: () => void;
    onCreated: (k: APIKeyWithSecret) => void;
}) {
    const perms = useAPIPermissions();
    const create = useCreateAPIKey();

    const [name, setName] = React.useState("");
    const [description, setDescription] = React.useState("");
    const [preset, setPreset] = React.useState<Preset>("read_only");
    const [permissionsBitmask, setPermissionsBitmask] = React.useState<number>(0);
    const [rateLimit, setRateLimit] = React.useState<number>(60);
    const [expiresIn, setExpiresIn] = React.useState<"never" | "30" | "90" | "365">("never");
    const [allowedIPsRaw, setAllowedIPsRaw] = React.useState("");
    const [advanced, setAdvanced] = React.useState(false);

    // Default the bitmask to whatever the read_only preset says, once perms load.
    React.useEffect(() => {
        if (perms.data && permissionsBitmask === 0 && preset !== "custom") {
            setPermissionsBitmask(presetMask(preset, perms.data.presets));
        }
    }, [perms.data, preset, permissionsBitmask]);

    function switchPreset(p: Preset) {
        setPreset(p);
        if (p !== "custom" && perms.data) {
            setPermissionsBitmask(presetMask(p, perms.data.presets));
        }
    }

    function togglePermission(value: number) {
        setPreset("custom");
        setPermissionsBitmask((m) => (m & value ? m & ~value : m | value));
    }

    async function submit() {
        if (!name.trim()) {
            toast.error("Name is required");
            return;
        }
        if (permissionsBitmask === 0) {
            toast.error("Pick at least one permission");
            return;
        }
        const allowedIPs = allowedIPsRaw
            .split(/[\s,]+/)
            .map((s) => s.trim())
            .filter(Boolean);

        const expiresAt =
            expiresIn === "never"
                ? undefined
                : new Date(Date.now() + Number(expiresIn) * 24 * 60 * 60 * 1000).toISOString();

        try {
            const k = await create.mutateAsync({
                name: name.trim(),
                description: description.trim() || undefined,
                permissions: permissionsBitmask,
                rate_limit_per_minute: rateLimit,
                expires_at: expiresAt,
                allowed_ips: allowedIPs.length > 0 ? allowedIPs : undefined,
            });
            onCreated(k);
        } catch (e) {
            const msg = e instanceof Error ? e.message : "Failed to create key";
            toast.error(msg);
        }
    }

    return (
        <div>
            <div className="h-11 px-4 border-b border-slate-200 flex items-center gap-2">
                <KeyIcon className="w-3.5 h-3.5 text-slate-500" />
                <span className="text-[12.5px] font-medium text-slate-900">Create API key</span>
                <button
                    type="button"
                    onClick={onCancel}
                    className="ml-auto w-7 h-7 rounded-md hover:bg-slate-100 inline-flex items-center justify-center text-slate-500 hover:text-slate-900"
                    aria-label="Close"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>

            <div className="px-5 py-4 space-y-4 max-h-[70dvh] overflow-y-auto">
                <Field label="Name" hint="Shown in the dashboard. e.g. production-server, ci-pipeline.">
                    <input
                        autoFocus
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        placeholder="production-server"
                        className="w-full h-8 px-2.5 rounded-md border border-slate-200 focus:border-sky-400 focus:ring-1 focus:ring-sky-200 outline-none text-[12.5px] text-slate-900 placeholder:text-slate-400"
                    />
                </Field>

                <Field label="Description" optional hint="Internal note. Where will this key be used?">
                    <textarea
                        value={description}
                        onChange={(e) => setDescription(e.target.value)}
                        rows={2}
                        placeholder="Server-side calls from EU production"
                        className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 focus:border-sky-400 focus:ring-1 focus:ring-sky-200 outline-none text-[12.5px] text-slate-900 placeholder:text-slate-400 resize-none"
                    />
                </Field>

                <Field label="Permissions">
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-1.5 mb-2">
                        <PresetCard
                            icon={<ShieldIcon className="w-3 h-3" />}
                            label="Read-only"
                            sub="View everything"
                            active={preset === "read_only"}
                            onClick={() => switchPreset("read_only")}
                        />
                        <PresetCard
                            icon={<ShieldCheckIcon className="w-3 h-3" />}
                            label="Full access"
                            sub="Read + write"
                            active={preset === "full_access"}
                            onClick={() => switchPreset("full_access")}
                        />
                        <PresetCard
                            icon={<SlidersIcon className="w-3 h-3" />}
                            label="Custom"
                            sub="Pick scopes"
                            active={preset === "custom"}
                            onClick={() => switchPreset("custom")}
                        />
                    </div>
                    {perms.isPending ? (
                        <div className="h-24 rounded-md bg-slate-50 animate-pulse" />
                    ) : perms.isError ? (
                        <div className="rounded-md border border-rose-200 bg-rose-50 px-3 py-3 text-[11.5px] text-rose-700 flex items-center justify-between gap-2">
                            <span>Couldn't load permissions.</span>
                            <button type="button" onClick={() => perms.refetch()} className="font-medium underline underline-offset-2 hover:no-underline">
                                Retry
                            </button>
                        </div>
                    ) : (perms.data?.permissions?.length ?? 0) === 0 ? (
                        <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-3 text-[11.5px] text-slate-500">
                            No permissions available for this account.
                        </div>
                    ) : (
                        <PermissionMatrix
                            permissions={perms.data!.permissions}
                            bitmask={permissionsBitmask}
                            onToggle={togglePermission}
                        />
                    )}
                </Field>

                <Field label="Rate limit" hint="Soft cap, sliding window. We return 429 + Retry-After when exceeded.">
                    <RateLimitSetter value={rateLimit} onChange={setRateLimit} />
                </Field>

                <Field label="Expires">
                    <div className="flex items-center gap-1">
                        {(["never", "30", "90", "365"] as const).map((v) => (
                            <button
                                key={v}
                                type="button"
                                onClick={() => setExpiresIn(v)}
                                className={`h-7 px-3 rounded-md text-[11.5px] transition-colors ${
                                    expiresIn === v
                                        ? "bg-slate-900 text-white"
                                        : "border border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                                }`}
                            >
                                {v === "never" ? "Never" : `${v} days`}
                            </button>
                        ))}
                    </div>
                </Field>

                <button
                    type="button"
                    onClick={() => setAdvanced((a) => !a)}
                    className="text-[11.5px] text-slate-500 hover:text-slate-900 underline-offset-2 hover:underline"
                >
                    {advanced ? "Hide advanced" : "Advanced (IP allowlist)"}
                </button>

                {advanced && (
                    <Field label="Allowed IPs" optional hint="Comma- or newline-separated. Supports CIDR (10.0.0.0/8). Empty = any.">
                        <textarea
                            value={allowedIPsRaw}
                            onChange={(e) => setAllowedIPsRaw(e.target.value)}
                            rows={2}
                            placeholder="203.0.113.10, 198.51.100.0/24"
                            className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 focus:border-sky-400 focus:ring-1 focus:ring-sky-200 outline-none text-[12px] text-slate-900 placeholder:text-slate-400 resize-none font-mono"
                        />
                    </Field>
                )}
            </div>

            <div className="h-12 px-4 border-t border-slate-200 flex items-center gap-2 bg-slate-50/40">
                <span className="hidden md:inline-flex text-[11px] text-slate-500 items-center gap-1.5">
                    <LockIcon className="w-3 h-3" />
                    The secret will be shown once. Save it somewhere safe.
                </span>
                <div className="ml-auto flex items-center gap-1.5">
                    <button
                        type="button"
                        onClick={onCancel}
                        className="h-7 px-3 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={submit}
                        disabled={create.isPending}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {create.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <KeyIcon className="w-3 h-3" />}
                        Create key
                    </button>
                </div>
            </div>
        </div>
    );
}

function RevealStep({ apiKey, onClose }: { apiKey: APIKeyWithSecret; onClose: () => void }) {
    const [copied, setCopied] = React.useState(false);
    function copy() {
        navigator.clipboard.writeText(apiKey.secret).then(
            () => {
                setCopied(true);
                toast.success("Secret copied to clipboard");
                setTimeout(() => setCopied(false), 2000);
            },
            () => toast.error("Failed to copy"),
        );
    }
    return (
        <div>
            <div className="h-11 px-4 border-b border-slate-200 flex items-center gap-2">
                <CheckIcon className="w-3.5 h-3.5 text-emerald-600" />
                <span className="text-[12.5px] font-medium text-slate-900">Key created</span>
            </div>

            <div className="px-5 py-5">
                <p className="text-[12.5px] text-slate-900 font-medium mb-1">{apiKey.name}</p>
                <p className="text-[11.5px] text-slate-500 mb-4">
                    Copy the secret now. We hash it on the server and can never show it again.
                </p>

                <div className="rounded-md border border-slate-200 bg-slate-950 overflow-hidden">
                    <div className="px-3 py-2 flex items-center gap-2 border-b border-slate-800/60">
                        <KeyIcon className="w-3 h-3 text-slate-400" />
                        <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-slate-400">
                            Secret · once-only
                        </span>
                        <button
                            type="button"
                            onClick={copy}
                            className="ml-auto inline-flex items-center gap-1 h-6 px-2 rounded text-slate-300 hover:text-white hover:bg-slate-800 transition-colors text-[11px]"
                        >
                            {copied ? <CheckIcon className="w-3 h-3" /> : <CopyIcon className="w-3 h-3" />}
                            {copied ? "Copied" : "Copy"}
                        </button>
                    </div>
                    <pre className="px-4 py-3 text-[12px] text-slate-100 font-mono whitespace-pre-wrap break-all">
                        {apiKey.secret}
                    </pre>
                </div>

                <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[11.5px] text-amber-900 leading-relaxed">
                    <strong>Heads up:</strong> closing this dialog will discard the plaintext secret.
                    Anyone with this string can act as <span className="font-mono">{apiKey.name}</span>.
                </div>
            </div>

            <div className="h-12 px-4 border-t border-slate-200 flex items-center justify-end bg-slate-50/40">
                <button
                    type="button"
                    onClick={onClose}
                    className="h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium transition-colors"
                >
                    I've saved it
                </button>
            </div>
        </div>
    );
}

function Field({
    label,
    hint,
    optional,
    children,
}: {
    label: string;
    hint?: string;
    optional?: boolean;
    children: React.ReactNode;
}) {
    return (
        <div>
            <div className="flex items-center gap-2 mb-1">
                <label className="text-[11px] uppercase tracking-[0.14em] text-slate-500 font-medium">
                    {label}
                </label>
                {optional && (
                    <span className="text-[10px] text-slate-400 font-mono">optional</span>
                )}
            </div>
            {children}
            {hint && <p className="text-[10.5px] text-slate-400 mt-1 leading-relaxed">{hint}</p>}
        </div>
    );
}

function RateLimitSetter({ value, onChange }: { value: number; onChange: (v: number) => void }) {
    const clamp = (n: number) => Math.max(1, Math.min(10000, Math.round(n) || 1));
    const presets = [30, 60, 120, 600, 1000];
    const stepBtn =
        "w-8 h-8 rounded-md border border-slate-200 text-slate-500 hover:text-slate-900 hover:border-slate-300 inline-flex items-center justify-center transition-colors disabled:opacity-40 disabled:hover:text-slate-500 disabled:hover:border-slate-200";

    return (
        <div className="rounded-lg border border-slate-200 bg-white px-3 py-3">
            <div className="flex items-center justify-center gap-3">
                <button type="button" onClick={() => onChange(clamp(value - 10))} disabled={value <= 1} className={stepBtn} aria-label="Decrease by 10">
                    <MinusIcon className="w-3.5 h-3.5" />
                </button>
                <div className="flex items-baseline gap-1.5">
                    <input
                        type="number"
                        min={1}
                        max={10000}
                        value={value}
                        onChange={(e) => onChange(clamp(Number(e.target.value)))}
                        className="w-[74px] text-center text-[22px] font-semibold tabular-nums text-slate-900 bg-transparent outline-none [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
                    />
                    <span className="text-[11px] text-slate-400 whitespace-nowrap">req / min</span>
                </div>
                <button type="button" onClick={() => onChange(clamp(value + 10))} disabled={value >= 10000} className={stepBtn} aria-label="Increase by 10">
                    <PlusIcon className="w-3.5 h-3.5" />
                </button>
            </div>

            <input
                type="range"
                min={1}
                max={1000}
                step={1}
                value={Math.min(value, 1000)}
                onChange={(e) => onChange(clamp(Number(e.target.value)))}
                className="w-full mt-3 h-1.5 accent-sky-600 cursor-pointer"
            />

            <div className="flex items-center justify-center gap-1.5 mt-2.5">
                {presets.map((v) => (
                    <button
                        key={v}
                        type="button"
                        onClick={() => onChange(v)}
                        className={`h-6 px-2.5 rounded-md text-[11px] tabular-nums transition-colors ${
                            value === v
                                ? "bg-sky-600 text-white"
                                : "border border-slate-200 text-slate-600 hover:text-slate-900 hover:border-slate-300"
                        }`}
                    >
                        {v >= 1000 ? "1k" : v}
                    </button>
                ))}
            </div>
        </div>
    );
}

function PresetCard({
    icon,
    label,
    sub,
    active,
    onClick,
}: {
    icon: React.ReactNode;
    label: string;
    sub: string;
    active: boolean;
    onClick: () => void;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className={`relative text-left rounded-md border px-2.5 py-2 transition-colors ${
                active
                    ? "border-sky-400 bg-sky-50/60"
                    : "border-slate-200 hover:border-slate-300 bg-white"
            }`}
        >
            <div className="flex items-center gap-1.5 mb-0.5">
                <span className={active ? "text-sky-600" : "text-slate-500"}>{icon}</span>
                <span className={`text-[11.5px] font-medium ${active ? "text-sky-900" : "text-slate-900"}`}>
                    {label}
                </span>
            </div>
            <p className="text-[10.5px] text-slate-500">{sub}</p>
        </button>
    );
}

function PermissionMatrix({
    permissions,
    bitmask,
    onToggle,
}: {
    permissions: APIPermission[];
    bitmask: number;
    onToggle: (value: number) => void;
}) {
    const grouped = React.useMemo(() => {
        const out: Record<string, APIPermission[]> = {};
        for (const p of permissions) {
            (out[p.category] ?? (out[p.category] = [])).push(p);
        }
        return out;
    }, [permissions]);

    // Render the known categories in a sensible order, then ANY others the API
    // returns — so a new/renamed category can never silently hide its scopes.
    const categories = React.useMemo(() => {
        const known = ["read", "write", "bulk", "special"];
        const ordered = known.filter((c) => grouped[c]?.length);
        const extra = Object.keys(grouped).filter((c) => !known.includes(c));
        return [...ordered, ...extra];
    }, [grouped]);

    return (
        <div className="rounded-md border border-slate-200 divide-y divide-slate-200/60 max-h-56 overflow-y-auto">
            {categories.map((cat) =>
                grouped[cat] && grouped[cat].length > 0 ? (
                    <div key={cat}>
                        <div className="px-2.5 py-1.5 bg-slate-50/60 text-[10px] uppercase tracking-[0.14em] text-slate-500 font-medium">
                            {cat}
                        </div>
                        <div>
                            {grouped[cat].map((p) => {
                                const on = (bitmask & p.value) !== 0;
                                return (
                                    <button
                                        key={p.name}
                                        type="button"
                                        onClick={() => onToggle(p.value)}
                                        className="w-full px-2.5 py-1.5 flex items-center gap-2 hover:bg-slate-50 text-left transition-colors"
                                    >
                                        <div
                                            className={`w-3.5 h-3.5 rounded border flex items-center justify-center transition-colors shrink-0 ${
                                                on
                                                    ? "border-sky-600 bg-sky-600 text-white"
                                                    : "border-slate-300 bg-white"
                                            }`}
                                        >
                                            {on && <CheckIcon className="w-2.5 h-2.5" strokeWidth={3} />}
                                        </div>
                                        <span className="font-mono text-[11px] text-slate-900">{p.name}</span>
                                        <span className="text-[10.5px] text-slate-500 truncate">{p.description}</span>
                                    </button>
                                );
                            })}
                        </div>
                    </div>
                ) : null,
            )}
        </div>
    );
}

function presetMask(p: Preset, presets: { read_only: number; full_access: number }): number {
    if (p === "read_only") return presets.read_only;
    if (p === "full_access") return presets.full_access;
    return 0;
}
