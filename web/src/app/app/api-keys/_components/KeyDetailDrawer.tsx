// Per-key detail drawer.
//
// Shows the live picture for one key:
//   - identification (prefix/suffix, status, created/last-used)
//   - configuration (permissions, rate limit, IP allowlist)
//   - usage graph (24h request volume, status-code split)
//   - top endpoints
//   - recent request log
//   - actions (revoke, edit name/description)
//
// Slides in from the right; closes on backdrop click or Escape.

import React from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
    ActivityIcon,
    AlertCircleIcon,
    CheckIcon,
    ChevronRightIcon,
    ClockIcon,
    GaugeIcon,
    Loader2Icon,
    NetworkIcon,
    RefreshCwIcon,
    ShieldCheckIcon,
    TrashIcon,
    XIcon,
} from "lucide-react";
import toast from "react-hot-toast";

import type APIKey from "@/lib/api/models/app/apikeys/APIKey";
import useAPIKeyAnalytics from "@/lib/api/hooks/app/api-keys/useAPIKeyAnalytics";
import useAPIKeyUsageLogs from "@/lib/api/hooks/app/api-keys/useAPIKeyUsageLogs";
import useAPIPermissions from "@/lib/api/hooks/app/api-keys/useAPIPermissions";
import useRevokeAPIKey from "@/lib/api/hooks/app/api-keys/useRevokeAPIKey";
import useUpdateAPIKey from "@/lib/api/hooks/app/api-keys/useUpdateAPIKey";
import { StackedBars } from "./Sparkline";
import { useConfirm } from "@/hooks/context/confirm";

export default function KeyDetailDrawer({
    apiKey,
    onClose,
}: {
    apiKey: APIKey | null;
    onClose: () => void;
}) {
    React.useEffect(() => {
        function onKey(e: KeyboardEvent) {
            if (e.key === "Escape") onClose();
        }
        window.addEventListener("keydown", onKey);
        return () => window.removeEventListener("keydown", onKey);
    }, [onClose]);

    return (
        <AnimatePresence>
            {apiKey && (
                <>
                    <motion.div
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.15 }}
                        className="fixed inset-0 z-40 bg-black/30"
                        onClick={onClose}
                    />
                    <motion.aside
                        initial={{ x: "100%" }}
                        animate={{ x: 0 }}
                        exit={{ x: "100%" }}
                        transition={{ type: "tween", ease: "easeOut", duration: 0.2 }}
                        className="fixed right-0 top-0 bottom-0 z-50 w-[640px] max-w-full bg-white border-l border-slate-200 shadow-[-20px_0_40px_-12px_rgba(15,23,42,0.16)] flex flex-col"
                    >
                        <Inner apiKey={apiKey} onClose={onClose} />
                    </motion.aside>
                </>
            )}
        </AnimatePresence>
    );
}

function Inner({ apiKey, onClose }: { apiKey: APIKey; onClose: () => void }) {
    const analytics = useAPIKeyAnalytics(apiKey.id);
    const logs = useAPIKeyUsageLogs(apiKey.id, { limit: 50 });
    const perms = useAPIPermissions();
    const revoke = useRevokeAPIKey();
    const update = useUpdateAPIKey();
    const confirm = useConfirm();

    const [editing, setEditing] = React.useState(false);
    const [name, setName] = React.useState(apiKey.name);
    const [description, setDescription] = React.useState(apiKey.description ?? "");

    React.useEffect(() => {
        setName(apiKey.name);
        setDescription(apiKey.description ?? "");
    }, [apiKey.id, apiKey.name, apiKey.description]);

    function saveEdit() {
        if (!name.trim()) {
            toast.error("Name is required");
            return;
        }
        update.mutate(
            {
                id: apiKey.id,
                data: { name: name.trim(), description: description.trim() || undefined },
            },
            {
                onSuccess: () => {
                    setEditing(false);
                    toast.success("Saved");
                },
                onError: () => toast.error("Failed to save"),
            },
        );
    }

    function confirmRevoke() {
        confirm.show(
            `Revoke "${apiKey.name}"? Requests authenticating with this key will start failing immediately.`,
            async () => {
                try {
                    await revoke.mutateAsync({ id: apiKey.id, reason: "Revoked by user" });
                    toast.success("Key revoked");
                    onClose();
                } catch {
                    toast.error("Failed to revoke");
                }
            },
        );
    }

    const grantedPermissions = React.useMemo(() => {
        if (!perms.data) return [];
        return perms.data.permissions.filter((p) => (apiKey.permissions & p.value) !== 0);
    }, [perms.data, apiKey.permissions]);

    return (
        <>
            {/* Header */}
            <div className="h-12 px-5 border-b border-slate-200 flex items-center gap-2 shrink-0">
                <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                    API key
                </span>
                <div className="h-4 w-px bg-slate-200" />
                <span className="font-mono text-[11.5px] text-slate-900 truncate">
                    {apiKey.key_prefix}…{apiKey.key_suffix}
                </span>
                <StatusBadge status={apiKey.status} />
                <button
                    type="button"
                    onClick={onClose}
                    className="ml-auto w-7 h-7 rounded-md hover:bg-slate-100 inline-flex items-center justify-center text-slate-500 hover:text-slate-900"
                    aria-label="Close"
                >
                    <XIcon className="w-3.5 h-3.5" />
                </button>
            </div>

            {/* Body (scrollable) */}
            <div className="flex-1 overflow-y-auto">
                {/* Identification */}
                <section className="px-5 py-4 border-b border-slate-200/60">
                    {editing ? (
                        <div className="space-y-2">
                            <input
                                autoFocus
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                className="w-full h-8 px-2.5 rounded-md border border-slate-200 focus:border-sky-400 focus:ring-1 focus:ring-sky-200 outline-none text-[12.5px]"
                            />
                            <textarea
                                value={description}
                                onChange={(e) => setDescription(e.target.value)}
                                rows={2}
                                placeholder="Description"
                                className="w-full px-2.5 py-1.5 rounded-md border border-slate-200 focus:border-sky-400 focus:ring-1 focus:ring-sky-200 outline-none text-[12px] resize-none"
                            />
                            <div className="flex items-center gap-1.5">
                                <button
                                    type="button"
                                    onClick={saveEdit}
                                    disabled={update.isPending}
                                    className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] inline-flex items-center gap-1.5 disabled:opacity-60"
                                >
                                    {update.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <CheckIcon className="w-3 h-3" />}
                                    Save
                                </button>
                                <button
                                    type="button"
                                    onClick={() => setEditing(false)}
                                    className="h-7 px-3 rounded-md border border-slate-200 text-[12px] text-slate-700 hover:border-slate-300"
                                >
                                    Cancel
                                </button>
                            </div>
                        </div>
                    ) : (
                        <div>
                            <div className="flex items-center gap-2">
                                <h2 className="text-[15px] font-medium text-slate-900 truncate">{apiKey.name}</h2>
                                {apiKey.status === "active" && (
                                    <button
                                        type="button"
                                        onClick={() => setEditing(true)}
                                        className="text-[11px] text-sky-700 hover:text-sky-900 underline-offset-2 hover:underline"
                                    >
                                        Edit
                                    </button>
                                )}
                            </div>
                            {apiKey.description && (
                                <p className="text-[12px] text-slate-500 mt-0.5 leading-relaxed">{apiKey.description}</p>
                            )}
                            <div className="mt-2 grid grid-cols-3 gap-3 text-[10.5px]">
                                <MiniField label="Created" value={fmtRelative(apiKey.created_at)} />
                                <MiniField label="Last used" value={apiKey.last_used_at ? fmtRelative(apiKey.last_used_at) : "never"} />
                                <MiniField label="Last IP" value={apiKey.last_request_ip || "—"} mono />
                            </div>
                        </div>
                    )}
                </section>

                {/* Quick stats */}
                <section className="grid grid-cols-3 border-b border-slate-200/60 bg-slate-50/40">
                    <QuickStat
                        label="Requests · 24h"
                        value={analytics.data?.total ?? 0}
                        icon={<ActivityIcon className="w-3 h-3" />}
                    />
                    <QuickStat
                        label="Errors · 24h"
                        value={analytics.data?.errors ?? 0}
                        icon={<AlertCircleIcon className="w-3 h-3" />}
                        accent={(analytics.data?.errors ?? 0) > 0 ? "rose" : "slate"}
                    />
                    <QuickStat
                        label="Rate limit"
                        value={`${apiKey.rate_limit_per_minute}/m`}
                        icon={<GaugeIcon className="w-3 h-3" />}
                        last
                    />
                </section>

                {/* Usage graph */}
                <section className="px-5 py-4 border-b border-slate-200/60">
                    <SectionLabel
                        title="Traffic · last 24 hours"
                        action={
                            <button
                                type="button"
                                onClick={() => analytics.refetch()}
                                className="text-slate-400 hover:text-slate-900 inline-flex items-center gap-1 text-[10.5px]"
                            >
                                {analytics.isFetching ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RefreshCwIcon className="w-3 h-3" />}
                                Refresh
                            </button>
                        }
                    />
                    {analytics.isPending ? (
                        <div className="h-[140px] rounded bg-slate-50 animate-pulse" />
                    ) : (
                        <StackedBars buckets={analytics.data?.buckets ?? []} height={140} />
                    )}
                    <div className="mt-2 flex items-center gap-3 text-[10.5px] text-slate-500">
                        <Legend color="bg-emerald-500/80" label="2xx success" />
                        <Legend color="bg-amber-400/80" label="4xx client" />
                        <Legend color="bg-rose-500/80" label="5xx server" />
                    </div>
                </section>

                {/* Top endpoints */}
                <section className="px-5 py-4 border-b border-slate-200/60">
                    <SectionLabel title="Top endpoints" />
                    {analytics.isPending ? (
                        <div className="h-16 rounded bg-slate-50 animate-pulse" />
                    ) : analytics.data?.endpoints && analytics.data.endpoints.length > 0 ? (
                        <div className="divide-y divide-slate-200/60 -mx-1.5">
                            {analytics.data.endpoints.slice(0, 8).map((e, i) => (
                                <div key={`${e.method}-${e.endpoint}-${i}`} className="px-1.5 py-1.5 flex items-center gap-2">
                                    <span className="text-[10px] uppercase font-mono text-slate-400 w-12 shrink-0">{e.method}</span>
                                    <span className="font-mono text-[11px] text-slate-900 truncate">{e.endpoint}</span>
                                    <span className="ml-auto tabular-nums font-mono text-[11px] text-slate-700">{e.count}</span>
                                    {e.error_count > 0 && (
                                        <span className="text-[10px] text-rose-600 tabular-nums">{e.error_count} err</span>
                                    )}
                                    <span className="hidden sm:inline text-[10px] text-slate-400 tabular-nums w-12 text-right">{Math.round(e.avg_latency_ms)}ms</span>
                                </div>
                            ))}
                        </div>
                    ) : (
                        <p className="text-[11.5px] text-slate-400">No endpoint traffic in this window.</p>
                    )}
                </section>

                {/* Permissions */}
                <section className="px-5 py-4 border-b border-slate-200/60">
                    <SectionLabel
                        title={`Permissions · ${grantedPermissions.length}`}
                        icon={<ShieldCheckIcon className="w-3 h-3" />}
                    />
                    {perms.isPending ? (
                        <div className="h-10 rounded bg-slate-50 animate-pulse" />
                    ) : (
                        <div className="flex flex-wrap gap-1">
                            {grantedPermissions.map((p) => (
                                <span
                                    key={p.name}
                                    className="inline-flex items-center gap-1 h-6 px-1.5 rounded text-[10.5px] font-mono border border-slate-200 text-slate-700 bg-white"
                                    title={p.description}
                                >
                                    <span
                                        className={`size-1.5 rounded-full ${
                                            p.category === "read"
                                                ? "bg-sky-400"
                                                : p.category === "write"
                                                  ? "bg-violet-400"
                                                  : p.category === "bulk"
                                                    ? "bg-amber-400"
                                                    : "bg-emerald-400"
                                        }`}
                                    />
                                    {p.name}
                                </span>
                            ))}
                            {grantedPermissions.length === 0 && (
                                <span className="text-[11.5px] text-slate-400">no scopes granted</span>
                            )}
                        </div>
                    )}
                </section>

                {/* IP allowlist */}
                {apiKey.allowed_ips && apiKey.allowed_ips.length > 0 && (
                    <section className="px-5 py-4 border-b border-slate-200/60">
                        <SectionLabel title="IP allowlist" icon={<NetworkIcon className="w-3 h-3" />} />
                        <div className="flex flex-wrap gap-1">
                            {apiKey.allowed_ips.map((ip) => (
                                <span
                                    key={ip}
                                    className="h-6 px-1.5 rounded text-[10.5px] font-mono border border-slate-200 text-slate-700 bg-white"
                                >
                                    {ip}
                                </span>
                            ))}
                        </div>
                    </section>
                )}

                {/* Activity log */}
                <section className="px-5 py-4">
                    <SectionLabel
                        title={`Recent activity${logs.data ? ` · ${logs.data.data.length}` : ""}`}
                        icon={<ClockIcon className="w-3 h-3" />}
                    />
                    {logs.isPending ? (
                        <div className="space-y-1">
                            {[0, 1, 2, 3].map((i) => (
                                <div key={i} className="h-7 rounded bg-slate-50 animate-pulse" />
                            ))}
                        </div>
                    ) : logs.data?.data && logs.data.data.length > 0 ? (
                        <div className="divide-y divide-slate-200/60 -mx-1.5">
                            {logs.data.data.map((l) => (
                                <div key={l.id} className="px-1.5 py-1.5 flex items-center gap-2">
                                    <StatusDot status={l.response_code} />
                                    <span className="hidden sm:inline font-mono text-[10px] tabular-nums text-slate-500 w-32 shrink-0">
                                        {fmtFull(l.created_at)}
                                    </span>
                                    <span className="sm:hidden font-mono text-[10px] tabular-nums text-slate-500 w-14 shrink-0">
                                        {fmtTime(l.created_at)}
                                    </span>
                                    <span className="text-[10px] uppercase font-mono text-slate-400 w-10 shrink-0">{l.method}</span>
                                    <span className="font-mono text-[11px] text-slate-900 truncate flex-1">{l.endpoint}</span>
                                    <span className="font-mono text-[10px] text-slate-500 tabular-nums w-10 text-right">{l.response_code}</span>
                                    <span className="hidden sm:inline font-mono text-[10px] text-slate-400 tabular-nums w-12 text-right">{l.response_time_ms}ms</span>
                                </div>
                            ))}
                        </div>
                    ) : (
                        <p className="text-[11.5px] text-slate-400">No requests recorded yet.</p>
                    )}
                </section>
            </div>

            {/* Footer */}
            <div className="h-12 px-4 border-t border-slate-200 flex items-center bg-white shrink-0">
                {apiKey.status === "active" ? (
                    <button
                        type="button"
                        onClick={confirmRevoke}
                        disabled={revoke.isPending}
                        className="h-7 px-3 rounded-md border border-rose-200 text-rose-700 hover:bg-rose-50 hover:border-rose-300 text-[12px] inline-flex items-center gap-1.5 transition-colors disabled:opacity-60"
                    >
                        {revoke.isPending ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <TrashIcon className="w-3 h-3" />}
                        Revoke key
                    </button>
                ) : (
                    <span className="text-[11.5px] text-slate-500 inline-flex items-center gap-1.5">
                        <span className="size-1.5 rounded-full bg-rose-500" />
                        Revoked {apiKey.revoked_at ? fmtRelative(apiKey.revoked_at) : ""}
                        {apiKey.revoked_reason ? ` · ${apiKey.revoked_reason}` : ""}
                    </span>
                )}
                <ChevronRightIcon className="w-3 h-3 text-slate-300 ml-auto" />
            </div>
        </>
    );
}

function StatusBadge({ status }: { status: "active" | "revoked" | "expired" }) {
    const tone =
        status === "active"
            ? { bg: "bg-emerald-50", text: "text-emerald-700", dot: "bg-emerald-500" }
            : status === "revoked"
              ? { bg: "bg-rose-50", text: "text-rose-700", dot: "bg-rose-500" }
              : { bg: "bg-slate-100", text: "text-slate-600", dot: "bg-slate-400" };
    return (
        <span className={`inline-flex items-center gap-1 h-5 px-1.5 rounded text-[10px] uppercase tracking-[0.08em] font-medium ${tone.bg} ${tone.text}`}>
            <span className={`size-1.5 rounded-full ${tone.dot}`} />
            {status}
        </span>
    );
}

function StatusDot({ status }: { status: number }) {
    const c = status >= 500 ? "bg-rose-500" : status >= 400 ? "bg-amber-500" : status >= 200 ? "bg-emerald-500" : "bg-slate-400";
    return <span className={`size-1.5 rounded-full ${c} shrink-0`} />;
}

function MiniField({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
    return (
        <div>
            <div className="text-[9.5px] uppercase tracking-[0.14em] text-slate-400 font-medium">{label}</div>
            <div className={`mt-0.5 text-[11.5px] text-slate-700 ${mono ? "font-mono" : ""} truncate`}>{value}</div>
        </div>
    );
}

function QuickStat({
    label,
    value,
    icon,
    accent = "slate",
    last,
}: {
    label: string;
    value: number | string;
    icon: React.ReactNode;
    accent?: "slate" | "rose";
    last?: boolean;
}) {
    return (
        <div className={`px-3 md:px-5 py-3 ${!last ? "border-r border-slate-200/60" : ""}`}>
            <div className="flex items-center gap-1.5">
                <span className={`text-[10px] uppercase tracking-[0.14em] font-medium ${accent === "rose" ? "text-rose-500" : "text-slate-400"}`}>
                    {label}
                </span>
                <span className={`${accent === "rose" ? "text-rose-500" : "text-slate-300"}`}>{icon}</span>
            </div>
            <div className={`mt-1 text-[20px] leading-none font-light tabular-nums ${accent === "rose" ? "text-rose-700" : "text-slate-900"}`}>
                {typeof value === "number" ? value.toLocaleString() : value}
            </div>
        </div>
    );
}

function SectionLabel({
    title,
    icon,
    action,
}: {
    title: string;
    icon?: React.ReactNode;
    action?: React.ReactNode;
}) {
    return (
        <div className="flex items-center gap-1.5 mb-2">
            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-500 font-medium inline-flex items-center gap-1.5">
                {icon}
                {title}
            </span>
            {action && <span className="ml-auto">{action}</span>}
        </div>
    );
}

function Legend({ color, label }: { color: string; label: string }) {
    return (
        <span className="inline-flex items-center gap-1">
            <span className={`size-1.5 rounded-sm ${color}`} />
            {label}
        </span>
    );
}

function fmtRelative(iso: string): string {
    try {
        const d = new Date(iso);
        const diff = Date.now() - d.getTime();
        const s = Math.floor(diff / 1000);
        if (s < 60) return `${s}s ago`;
        const m = Math.floor(s / 60);
        if (m < 60) return `${m}m ago`;
        const h = Math.floor(m / 60);
        if (h < 24) return `${h}h ago`;
        const days = Math.floor(h / 24);
        if (days < 30) return `${days}d ago`;
        return d.toLocaleDateString();
    } catch {
        return "—";
    }
}

function fmtFull(iso: string): string {
    try {
        const d = new Date(iso);
        return d.toLocaleString("en-US", { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit" });
    } catch {
        return "—";
    }
}

// Compact HH:MM for the mobile activity log, where the full timestamp
// would crowd out the endpoint path.
function fmtTime(iso: string): string {
    try {
        const d = new Date(iso);
        return d.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit", hour12: false });
    } catch {
        return "—";
    }
}
