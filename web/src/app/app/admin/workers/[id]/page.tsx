import { useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    convertWorkerToDedicated,
    deleteWorker,
    setWorkerRiskPool,
    getManagedWorker,
    getWorkerLiveStatus,
    getWorkerLogs,
    installWorker,
    listManagedWorkers,
    listWorkerEmails,
    rebootWorker,
    reassignEmailsToWorker,
    restartWorker,
    rotateWorkerKeys,
    systemUpdateWorker,
    testWorker,
    uninstallWorker,
    upgradeWorker,
} from "@/lib/api/client/app/admin/workers";
import {
    applyWorkerConfig,
    assignWorkerProfile,
    listWorkerProfiles,
} from "@/lib/api/client/app/admin/credentials";
import { SmartLabel, TagEditor, smartLabels } from "../../_components/WorkerLabels";

export default function AdminWorkerDetailPage() {
    const { id = "" } = useParams();
    const nav = useNavigate();
    const qc = useQueryClient();

    const worker = useQuery({
        queryKey: ["admin", "worker", id],
        queryFn: () => getManagedWorker(id),
        enabled: !!id,
    });

    const [actionMsg, setActionMsg] = useState<string | null>(null);
    const [logs, setLogs] = useState<string>("");
    const [liveStatus, setLiveStatus] = useState<string>("");
    const [sysUpdateOut, setSysUpdateOut] = useState<string>("");
    const [sysRebootNeeded, setSysRebootNeeded] = useState<boolean>(false);
    const [rewireTarget, setRewireTarget] = useState<string>("");
    const [rewireMsg, setRewireMsg] = useState<string | null>(null);
    const [convertOpen, setConvertOpen] = useState(false);
    const [convertUser, setConvertUser] = useState("");
    const [convertSub, setConvertSub] = useState("");
    const [convertDrain, setConvertDrain] = useState("");
    const [convertMsg, setConvertMsg] = useState<string | null>(null);
    const [converting, setConverting] = useState(false);

    const allWorkers = useQuery({
        queryKey: ["admin", "workers", "managed"],
        queryFn: listManagedWorkers,
    });

    function refresh() {
        qc.invalidateQueries({ queryKey: ["admin", "worker", id] });
    }

    const opts = (label: string) => ({
        onSuccess: () => {
            setActionMsg(`${label}: ok`);
            refresh();
        },
        onError: (e: any) => setActionMsg(`${label}: ${e?.message || "failed"}`),
    });

    const test = useMutation({ mutationFn: () => testWorker(id), ...opts("test") });
    const install = useMutation({ mutationFn: () => installWorker(id), ...opts("install") });
    const restart = useMutation({ mutationFn: () => restartWorker(id), ...opts("restart") });
    const upgrade = useMutation({ mutationFn: () => upgradeWorker(id), ...opts("upgrade") });
    const uninstall = useMutation({ mutationFn: () => uninstallWorker(id), ...opts("uninstall") });
    const apply = useMutation({ mutationFn: () => applyWorkerConfig(id), ...opts("apply") });
    const setPool = useMutation({
        mutationFn: (pool: "clean" | "risky" | "quarantine") => setWorkerRiskPool(id, pool),
        ...opts("set risk pool"),
    });

    const profiles = useQuery({ queryKey: ["admin", "profiles"], queryFn: listWorkerProfiles });
    const assignProfile = useMutation({
        mutationFn: (profileID: string | null) => assignWorkerProfile(id, profileID),
        ...opts("assign profile"),
    });
    const rotate = useMutation({
        mutationFn: () => rotateWorkerKeys(id),
        onSuccess: (r) => {
            setActionMsg(`rotated. New key:\n${r.ssh_public_key}`);
            refresh();
        },
        onError: (e: any) => setActionMsg(`rotate: ${e?.message || "failed"}`),
    });
    const removeWorker = useMutation({
        mutationFn: () => deleteWorker(id),
        onSuccess: () => nav("/app/admin/workers"),
    });

    async function fetchLogs() {
        try {
            const r = await getWorkerLogs(id, 200);
            setLogs(r.logs);
        } catch (e: any) {
            setLogs("error: " + (e?.message || "failed"));
        }
    }

    async function runSystemUpdate() {
        if (!confirm("Run OS package upgrade on this worker? This can take several minutes.")) return;
        setSysUpdateOut("running…");
        setSysRebootNeeded(false);
        try {
            const r = await systemUpdateWorker(id);
            setSysUpdateOut(r.output);
            setSysRebootNeeded(r.reboot_required);
        } catch (e: any) {
            setSysUpdateOut("error: " + (e?.message || "failed"));
        }
    }

    async function rewireAll() {
        if (!rewireTarget) return;
        if (!confirm("Move every email account on this worker to the selected target?")) return;
        setRewireMsg("fetching accounts…");
        try {
            const emails = await listWorkerEmails(id);
            if (emails.data.length === 0) {
                setRewireMsg("no accounts to move");
                return;
            }
            setRewireMsg(`moving ${emails.data.length} account${emails.data.length === 1 ? "" : "s"}…`);
            await reassignEmailsToWorker(rewireTarget, emails.data.map((e) => e.id));
            setRewireMsg(`✓ ${emails.data.length} moved`);
            refresh();
            allWorkers.refetch();
        } catch (e: any) {
            setRewireMsg("error: " + (e?.message || "failed"));
        }
    }

    async function doConvertToDedicated() {
        if (!convertUser || !convertSub) {
            setConvertMsg("user_id and subscription_id are required");
            return;
        }
        setConverting(true);
        setConvertMsg("converting…");
        try {
            const r = await convertWorkerToDedicated(id, {
                user_id: convertUser,
                subscription_id: convertSub,
                drain_to_worker_id: convertDrain || null,
            });
            setConvertMsg(
                `✓ converted · ${r.accounts_drained} account${r.accounts_drained === 1 ? "" : "s"} drained · ${r.new_assignment ? "newly assigned" : "already assigned"}`,
            );
            refresh();
            allWorkers.refetch();
        } catch (e: any) {
            setConvertMsg("error: " + (e?.message || "failed"));
        } finally {
            setConverting(false);
        }
    }

    async function rebootHost() {
        if (!confirm("Reboot the VPS? It will be offline for ~1 minute.")) return;
        try {
            await rebootWorker(id);
            setActionMsg("reboot scheduled (≈60s)");
        } catch (e: any) {
            setActionMsg("reboot: " + (e?.message || "failed"));
        }
    }

    async function fetchLiveStatus() {
        try {
            const r = await getWorkerLiveStatus(id);
            setLiveStatus(
                `service active: ${r.service_active}\ncontainer up: ${r.container_up}\nimage: ${r.container_image}\nuptime: ${r.uptime}`,
            );
        } catch (e: any) {
            setLiveStatus("error: " + (e?.message || "failed"));
        }
    }

    if (worker.isLoading) return <p className="text-slate-400 text-sm">Loading…</p>;
    if (worker.error) return <p className="text-red-600 text-sm">Failed to load.</p>;
    const w = worker.data;
    if (!w) return null;

    return (
        <div className="space-y-6">
            <div>
                <button
                    onClick={() => nav("/app/admin/workers")}
                    className="text-slate-500 text-sm hover:underline"
                >
                    ← back
                </button>
                <h2 className="text-slate-700 font-semibold text-lg mt-1">{w.name || w.id}</h2>
                <p className="text-slate-400 text-xs font-mono">{w.id}</p>
            </div>

            <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                <Stat label="Install state" value={w.install_state} />
                <Stat label="Tier" value={w.worker_type + (w.worker_type === "shared" ? (w.free_tier ? " / free" : " / premium") : "")} />
                <Stat label="Accounts" value={String(w.account_count)} />
                <Stat label="Last seen" value={w.last_seen_at ? new Date(w.last_seen_at).toLocaleString() : "—"} />
                <Stat label="Running version" value={w.image_version || "—"} />
                {(() => {
                    const profile = profiles.data?.data?.find((p) => p.id === w.profile_id);
                    if (!profile || !profile.resolved_image_tag) return null;
                    const behind = w.image_version && w.image_version !== profile.resolved_image_tag;
                    return (
                        <Stat
                            label="Target version"
                            value={
                                profile.resolved_image_tag +
                                (behind ? "  ← update available" : "")
                            }
                        />
                    );
                })()}
            </div>

            <Section title="Connection">
                <dl className="text-sm grid grid-cols-[120px_1fr] gap-y-1">
                    <dt className="text-slate-500">Host</dt><dd className="font-mono">{w.ssh_host}</dd>
                    <dt className="text-slate-500">Port</dt><dd className="font-mono">{w.ssh_port}</dd>
                    <dt className="text-slate-500">User</dt><dd className="font-mono">{w.ssh_user}</dd>
                    <dt className="text-slate-500">Host fingerprint</dt>
                    <dd className="font-mono text-xs">{w.ssh_host_fingerprint || "(not yet pinned — click Test)"}</dd>
                </dl>
                {w.last_error && (
                    <pre className="mt-3 bg-red-50 text-red-700 text-xs p-2 rounded whitespace-pre-wrap">{w.last_error}</pre>
                )}
            </Section>

            <Section title="Tags">
                <div className="flex flex-wrap gap-1 mb-3">
                    {smartLabels(w).map((l) => (
                        <SmartLabel key={l} label={l} />
                    ))}
                </div>
                <p className="text-slate-400 text-xs mb-3">
                    Smart labels above are computed from the worker's state. Add your own free-form
                    tags below for region, provider, role, customer cohort, anything.
                </p>
                <TagEditor
                    worker={w}
                    onSaved={(tags) => {
                        qc.setQueryData(["admin", "worker", id], { ...w, tags });
                        qc.invalidateQueries({ queryKey: ["admin", "workers", "managed"] });
                        qc.invalidateQueries({ queryKey: ["admin", "worker-tags"] });
                    }}
                />
            </Section>

            <Section title="Worker profile">
                <div className="flex items-center gap-3">
                    <select
                        value={w.profile_id ?? ""}
                        onChange={(e) => assignProfile.mutate(e.target.value || null)}
                        disabled={assignProfile.isPending}
                        className="border rounded px-3 py-1.5 text-sm"
                    >
                        <option value="">(use backend env defaults)</option>
                        {profiles.data?.data?.map((p) => (
                            <option key={p.id} value={p.id}>
                                {p.name} · {p.app_env}
                            </option>
                        ))}
                    </select>
                    {w.profile_id && (
                        <Btn onClick={() => apply.mutate()} disabled={apply.isPending} primary>
                            Apply config & restart
                        </Btn>
                    )}
                </div>
                {(() => {
                    if (!w.profile_id) {
                        return (
                            <p className="text-slate-400 text-xs mt-2">
                                No profile assigned — installer reads Kafka/Schema/Redis/AWS from the backend's
                                own process env at install time.
                            </p>
                        );
                    }
                    const profile = profiles.data?.data?.find((p) => p.id === w.profile_id);
                    const stale =
                        profile &&
                        w.config_applied_at &&
                        new Date(profile.updated_at) > new Date(w.config_applied_at);
                    return (
                        <p className={`text-xs mt-2 ${stale ? "text-amber-600 font-medium" : "text-slate-400"}`}>
                            {stale
                                ? "⚠ Profile has been edited since this worker last applied. Click Apply to refresh."
                                : w.config_applied_at
                                ? `Last applied ${new Date(w.config_applied_at).toLocaleString()}`
                                : "Never applied — run Install to bootstrap."}
                        </p>
                    );
                })()}
            </Section>

            <Section title="SSH public key">
                <textarea
                    readOnly
                    rows={3}
                    value={w.ssh_public_key || ""}
                    className="w-full font-mono text-xs p-2 border rounded bg-slate-50"
                    onFocus={(e) => e.currentTarget.select()}
                />
                <p className="text-slate-500 text-xs mt-1">
                    Paste this into <code>~/.ssh/authorized_keys</code> on the VPS.
                </p>
            </Section>

            <Section title="Actions">
                <div className="flex flex-wrap gap-2">
                    <Btn onClick={() => test.mutate()} disabled={test.isPending}>Test connection</Btn>
                    <Btn onClick={() => install.mutate()} disabled={install.isPending} primary>Install / re-provision</Btn>
                    <Btn onClick={() => restart.mutate()} disabled={restart.isPending}>Restart</Btn>
                    <Btn onClick={() => upgrade.mutate()} disabled={upgrade.isPending}>Pull latest & restart</Btn>
                    <Btn onClick={() => uninstall.mutate()} disabled={uninstall.isPending}>Uninstall</Btn>
                    <Btn onClick={() => rotate.mutate()} disabled={rotate.isPending}>Rotate SSH keys</Btn>
                    <Btn onClick={() => { if (confirm("Delete this worker?")) removeWorker.mutate(); }} disabled={removeWorker.isPending} danger>
                        Delete
                    </Btn>
                </div>
                {actionMsg && (
                    <pre className="mt-3 bg-slate-100 text-xs p-2 rounded whitespace-pre-wrap">{actionMsg}</pre>
                )}
            </Section>

            <Section title="Live status">
                <Btn onClick={fetchLiveStatus}>Refresh</Btn>
                <pre className="mt-3 bg-slate-100 text-xs p-2 rounded whitespace-pre-wrap min-h-[60px]">{liveStatus || "(click refresh)"}</pre>
            </Section>

            {w.worker_type === "shared" && (
                <Section title="Risk pool">
                    <p className="text-slate-500 text-sm mb-2">
                        Buckets shared workers so high-risk mailboxes don't poison the reputation of
                        clean ones. The background rebalancer migrates mailboxes between pools
                        hourly based on their warmup health state.
                    </p>
                    <div className="flex gap-2 flex-wrap">
                        {(["clean", "risky", "quarantine"] as const).map((p) => (
                            <button
                                key={p}
                                onClick={() => {
                                    if (p === w.risk_pool) return;
                                    if (!confirm(`Move this worker to the ${p} pool? The rebalancer will redistribute mailboxes on the next tick.`)) return;
                                    setPool.mutate(p);
                                }}
                                disabled={setPool.isPending}
                                className={`px-3 py-1.5 rounded text-sm border disabled:opacity-50 ${
                                    p === w.risk_pool
                                        ? p === "clean"
                                            ? "bg-green-600 text-white border-green-600"
                                            : p === "risky"
                                            ? "bg-amber-600 text-white border-amber-600"
                                            : "bg-red-600 text-white border-red-600"
                                        : "bg-white text-slate-700 hover:bg-slate-50"
                                }`}
                            >
                                {p}
                            </button>
                        ))}
                    </div>
                    <p className="text-slate-400 text-xs mt-2">
                        Currently: <span className="font-mono">{w.risk_pool}</span>
                    </p>
                </Section>
            )}

            <Section title="Move accounts to another worker">
                <p className="text-slate-500 text-sm mb-2">
                    Useful when this worker is down or overloaded. Eligible targets are workers of
                    the same tier listed least-loaded first.
                </p>
                {(() => {
                    const candidates = (allWorkers.data?.data ?? [])
                        .filter((other) => other.id !== id)
                        .filter((other) => other.worker_type === w.worker_type && other.free_tier === w.free_tier)
                        .filter((other) => other.install_state === "installed")
                        .sort((a, b) => a.account_count - b.account_count);
                    return (
                        <div className="flex flex-wrap items-center gap-2">
                            <select
                                value={rewireTarget}
                                onChange={(e) => setRewireTarget(e.target.value)}
                                className="border rounded px-3 py-1.5 text-sm min-w-[18rem]"
                            >
                                <option value="">— pick a target worker —</option>
                                {candidates.map((c) => (
                                    <option key={c.id} value={c.id}>
                                        {c.name || c.id.slice(0, 8)} · {c.account_count} accounts · {c.ssh_host || c.ip_addr}
                                    </option>
                                ))}
                            </select>
                            <Btn onClick={rewireAll} disabled={!rewireTarget} primary>
                                Move all {w.account_count} account{w.account_count === 1 ? "" : "s"}
                            </Btn>
                            {candidates.length === 0 && (
                                <span className="text-slate-400 text-xs">
                                    No eligible targets of the same tier are healthy.
                                </span>
                            )}
                        </div>
                    );
                })()}
                {rewireMsg && (
                    <pre className="mt-3 bg-slate-100 text-xs p-2 rounded whitespace-pre-wrap">{rewireMsg}</pre>
                )}
            </Section>

            {w.worker_type === "shared" && (
                <Section title="Convert to dedicated">
                    <p className="text-slate-500 text-sm mb-2">
                        Flip this worker from the shared pool into a dedicated worker bound to one
                        organization. Any current accounts must be evicted to another worker first.
                    </p>
                    {!convertOpen ? (
                        <Btn onClick={() => setConvertOpen(true)} primary>
                            Convert this worker to dedicated…
                        </Btn>
                    ) : (
                        <div className="border rounded p-3 bg-slate-50">
                            <Field label="User / organization ID">
                                <input
                                    value={convertUser}
                                    onChange={(e) => setConvertUser(e.target.value)}
                                    placeholder="uuid (find on Users tab)"
                                    className="w-full border rounded px-3 py-1.5 text-sm font-mono"
                                />
                            </Field>
                            <Field label="Subscription ID">
                                <input
                                    value={convertSub}
                                    onChange={(e) => setConvertSub(e.target.value)}
                                    placeholder="uuid"
                                    className="w-full border rounded px-3 py-1.5 text-sm font-mono"
                                />
                            </Field>
                            <Field
                                label={
                                    w.account_count > 0
                                        ? `Drain ${w.account_count} account${w.account_count === 1 ? "" : "s"} to (required)`
                                        : "Drain target (not needed — this worker is empty)"
                                }
                            >
                                <select
                                    value={convertDrain}
                                    onChange={(e) => setConvertDrain(e.target.value)}
                                    className="w-full border rounded px-3 py-1.5 text-sm"
                                >
                                    <option value="">— pick a target —</option>
                                    {(allWorkers.data?.data ?? [])
                                        .filter((other) =>
                                            other.id !== id &&
                                            other.worker_type === "shared" &&
                                            other.install_state === "installed",
                                        )
                                        .sort((a, b) => a.account_count - b.account_count)
                                        .map((c) => (
                                            <option key={c.id} value={c.id}>
                                                {c.name || c.id.slice(0, 8)} · {c.account_count} accounts ·{" "}
                                                {c.free_tier ? "free" : "premium"}
                                            </option>
                                        ))}
                                </select>
                            </Field>
                            <div className="flex gap-2 mt-3">
                                <Btn onClick={doConvertToDedicated} disabled={converting} primary>
                                    {converting ? "Converting…" : "Convert"}
                                </Btn>
                                <button
                                    onClick={() => setConvertOpen(false)}
                                    className="border px-3 py-1.5 rounded text-sm"
                                >
                                    Cancel
                                </button>
                            </div>
                            {convertMsg && (
                                <pre className="mt-3 bg-white border text-xs p-2 rounded whitespace-pre-wrap">
                                    {convertMsg}
                                </pre>
                            )}
                        </div>
                    )}
                </Section>
            )}

            <Section title="System updates">
                <div className="flex gap-2 flex-wrap">
                    <Btn onClick={runSystemUpdate}>Update OS packages</Btn>
                    {sysRebootNeeded && (
                        <Btn onClick={rebootHost} primary>
                            Reboot required — reboot now
                        </Btn>
                    )}
                    {!sysRebootNeeded && (
                        <Btn onClick={rebootHost}>Reboot VPS</Btn>
                    )}
                </div>
                {sysUpdateOut && (
                    <pre className="mt-3 bg-slate-900 text-slate-100 text-xs p-3 rounded overflow-auto max-h-72 whitespace-pre-wrap">
                        {sysUpdateOut}
                    </pre>
                )}
                <p className="text-slate-400 text-xs mt-2">
                    Runs apt / dnf / pacman / apk upgrade depending on distro. Reboots are never automatic.
                </p>
            </Section>

            <Section title="Logs (last 200 lines)">
                <Btn onClick={fetchLogs}>Fetch logs</Btn>
                <pre className="mt-3 bg-slate-900 text-slate-100 text-xs p-3 rounded overflow-auto max-h-96 whitespace-pre-wrap">
                    {logs || "(click fetch)"}
                </pre>
            </Section>
        </div>
    );
}

function Stat({ label, value }: { label: string; value: string }) {
    return (
        <div className="border rounded-lg p-3">
            <div className="text-xs uppercase text-slate-400 font-medium">{label}</div>
            <div className="text-slate-700 font-medium mt-1 break-words">{value}</div>
        </div>
    );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
    return (
        <div>
            <h3 className="text-slate-600 text-sm font-semibold mb-2">{title}</h3>
            {children}
        </div>
    );
}

function Btn({
    children,
    onClick,
    disabled,
    primary,
    danger,
}: {
    children: React.ReactNode;
    onClick: () => void;
    disabled?: boolean;
    primary?: boolean;
    danger?: boolean;
}) {
    const base = "px-3 py-1.5 rounded text-sm border disabled:opacity-50";
    const variant = danger
        ? "bg-red-600 text-white border-red-600 hover:bg-red-700"
        : primary
        ? "bg-blue-600 text-white border-blue-600 hover:bg-blue-700"
        : "bg-white text-slate-700 hover:bg-slate-50";
    return (
        <button onClick={onClick} disabled={disabled} className={`${base} ${variant}`}>
            {children}
        </button>
    );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return (
        <div className="mb-2">
            <label className="text-xs font-medium text-slate-500 uppercase block mb-1">{label}</label>
            {children}
        </div>
    );
}
