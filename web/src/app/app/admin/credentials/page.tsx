import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
    applyProfileToAll,
    checkReleases,
    createAWSCredentials,
    createWorkerProfile,
    deleteAWSCredentials,
    deleteWorkerProfile,
    getReleasesState,
    listAWSCredentials,
    listWorkerProfiles,
    setProfileRelease,
    updateAWSCredentials,
    updateWorkerProfile,
} from "@/lib/api/client/app/admin/credentials";
import type {
    AWSCredentials,
    AWSCredentialsBody,
    WorkerProfile,
    WorkerProfileBody,
} from "@/lib/api/models/app/admin/Credentials";

type Tab = "aws" | "profiles";

export default function AdminCredentialsPage() {
    const [tab, setTab] = useState<Tab>("aws");

    return (
        <div>
            <div className="mb-4">
                <h2 className="text-slate-700 font-semibold text-lg">Credentials</h2>
                <p className="text-slate-400 text-sm">
                    Reusable secrets that workers run with. Stored encrypted; never shown after save.
                </p>
            </div>

            <div className="flex gap-2 mb-4 border-b">
                <TabBtn active={tab === "aws"} onClick={() => setTab("aws")}>AWS Credentials</TabBtn>
                <TabBtn active={tab === "profiles"} onClick={() => setTab("profiles")}>Worker Profiles</TabBtn>
            </div>

            {tab === "aws" ? <AWSCredsTab /> : <ProfilesTab />}
        </div>
    );
}

function TabBtn({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
    return (
        <button
            onClick={onClick}
            className={`px-3 py-2 text-sm font-medium border-b-2 -mb-px ${
                active ? "border-blue-600 text-blue-600" : "border-transparent text-slate-500 hover:text-slate-700"
            }`}
        >
            {children}
        </button>
    );
}

// AWS credentials tab

function AWSCredsTab() {
    const qc = useQueryClient();
    const { data, isLoading } = useQuery({ queryKey: ["admin", "aws-creds"], queryFn: listAWSCredentials });
    const [editing, setEditing] = useState<AWSCredentials | null>(null);
    const [creating, setCreating] = useState(false);

    return (
        <div>
            <div className="flex justify-end mb-3">
                <button
                    onClick={() => setCreating(true)}
                    className="bg-blue-600 hover:bg-blue-700 text-white px-3 py-1.5 rounded text-sm"
                >
                    + Add AWS credentials
                </button>
            </div>

            {isLoading && <p className="text-slate-400 text-sm">Loading…</p>}

            <table className="w-full text-sm border rounded overflow-hidden">
                <thead className="bg-slate-50 text-xs uppercase text-slate-500">
                    <tr>
                        <th className="text-left px-3 py-2">Name</th>
                        <th className="text-left px-3 py-2">Region</th>
                        <th className="text-left px-3 py-2">Access key</th>
                        <th className="text-left px-3 py-2">Secret</th>
                        <th className="text-right px-3 py-2">Actions</th>
                    </tr>
                </thead>
                <tbody>
                    {data?.data?.map((c) => (
                        <tr key={c.id} className="border-t">
                            <td className="px-3 py-2">
                                <div className="font-medium">{c.name}</div>
                                <div className="text-slate-400 text-xs">{c.description}</div>
                            </td>
                            <td className="px-3 py-2 font-mono text-xs">{c.region}</td>
                            <td className="px-3 py-2 font-mono text-xs">{c.access_key_id}</td>
                            <td className="px-3 py-2">{c.has_secret ? <span className="font-mono">••••••</span> : <span className="text-slate-400">—</span>}</td>
                            <td className="px-3 py-2 text-right">
                                <button onClick={() => setEditing(c)} className="text-blue-600 text-sm hover:underline mr-3">Edit</button>
                                <button
                                    onClick={async () => {
                                        if (!confirm(`Delete ${c.name}? Worker profiles referencing it will block this.`)) return;
                                        try {
                                            await deleteAWSCredentials(c.id);
                                            qc.invalidateQueries({ queryKey: ["admin", "aws-creds"] });
                                        } catch (e: any) {
                                            alert(e?.message || "delete failed");
                                        }
                                    }}
                                    className="text-red-600 text-sm hover:underline"
                                >
                                    Delete
                                </button>
                            </td>
                        </tr>
                    ))}
                    {data && data.data?.length === 0 && (
                        <tr><td colSpan={5} className="text-center text-slate-400 py-8 text-sm">No AWS credentials yet.</td></tr>
                    )}
                </tbody>
            </table>

            {(creating || editing) && (
                <AWSCredsForm
                    initial={editing}
                    onClose={() => { setEditing(null); setCreating(false); }}
                    onSaved={() => {
                        setEditing(null);
                        setCreating(false);
                        qc.invalidateQueries({ queryKey: ["admin", "aws-creds"] });
                    }}
                />
            )}
        </div>
    );
}

function AWSCredsForm({
    initial,
    onClose,
    onSaved,
}: {
    initial: AWSCredentials | null;
    onClose: () => void;
    onSaved: () => void;
}) {
    const [form, setForm] = useState<AWSCredentialsBody>({
        name: initial?.name ?? "",
        description: initial?.description ?? "",
        region: initial?.region ?? "us-east-1",
        access_key_id: initial?.access_key_id ?? "",
        secret_access_key: "",
    });
    const mut = useMutation({
        mutationFn: () =>
            initial ? updateAWSCredentials(initial.id, form) : createAWSCredentials(form),
        onSuccess: onSaved,
    });

    return (
        <Modal title={initial ? `Edit ${initial.name}` : "Add AWS credentials"} onClose={onClose}>
            <Field label="Name">
                <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} className={inp} required />
            </Field>
            <Field label="Description">
                <input value={form.description ?? ""} onChange={(e) => setForm({ ...form, description: e.target.value })} className={inp} />
            </Field>
            <Field label="Region">
                <input value={form.region} onChange={(e) => setForm({ ...form, region: e.target.value })} className={inp} />
            </Field>
            <Field label="Access key ID">
                <input value={form.access_key_id} onChange={(e) => setForm({ ...form, access_key_id: e.target.value })} className={inp} required />
            </Field>
            <Field
                label="Secret access key"
                hint={initial ? "Leave blank to keep the existing value." : "Stored encrypted via KMS."}
            >
                <input
                    type="password"
                    value={form.secret_access_key ?? ""}
                    onChange={(e) => setForm({ ...form, secret_access_key: e.target.value })}
                    placeholder={initial?.has_secret ? "••••••" : ""}
                    className={inp}
                />
            </Field>
            {mut.isError && <p className="text-red-600 text-sm">{(mut.error as any)?.message ?? "failed"}</p>}
            <div className="flex justify-end gap-2 mt-4">
                <button onClick={onClose} className="border px-3 py-1.5 rounded text-sm">Cancel</button>
                <button
                    onClick={() => mut.mutate()}
                    disabled={mut.isPending}
                    className="bg-blue-600 hover:bg-blue-700 text-white px-3 py-1.5 rounded text-sm disabled:opacity-50"
                >
                    {mut.isPending ? "Saving…" : "Save"}
                </button>
            </div>
        </Modal>
    );
}

// worker profiles tab

function ProfilesTab() {
    const qc = useQueryClient();
    const { data, isLoading } = useQuery({ queryKey: ["admin", "profiles"], queryFn: listWorkerProfiles });
    const aws = useQuery({ queryKey: ["admin", "aws-creds"], queryFn: listAWSCredentials });
    const releases = useQuery({ queryKey: ["admin", "releases-state"], queryFn: getReleasesState });
    const [editing, setEditing] = useState<WorkerProfile | null>(null);
    const [creating, setCreating] = useState(false);
    const [applyMsg, setApplyMsg] = useState<string | null>(null);

    async function manualCheck() {
        try {
            await checkReleases();
            qc.invalidateQueries({ queryKey: ["admin", "releases-state"] });
            qc.invalidateQueries({ queryKey: ["admin", "profiles"] });
        } catch (e: any) {
            alert(e?.message || "check failed");
        }
    }

    async function applyAll(p: WorkerProfile) {
        if (!confirm(`Re-apply config to all workers using ${p.name}?`)) return;
        try {
            const r = await applyProfileToAll(p.id);
            const okCount = r.results.filter((x) => x.ok).length;
            setApplyMsg(`${p.name}: ${okCount}/${r.results.length} workers applied. ${
                r.results.filter((x) => !x.ok).map((x) => x.error || x.skipped).join("; ")
            }`);
        } catch (e: any) {
            setApplyMsg(e?.message || "apply failed");
        }
    }

    return (
        <div>
            {releases.data?.enabled && (
                <div className="border rounded-lg p-3 mb-4 bg-slate-50">
                    <div className="flex items-start justify-between mb-2">
                        <div>
                            <h3 className="text-slate-700 font-semibold text-sm">Releases</h3>
                            <p className="text-slate-500 text-xs">
                                Tracking <code className="bg-white px-1 rounded">{releases.data.github_repo}</code>
                                {" → "}<code className="bg-white px-1 rounded">{releases.data.image_repo}</code>
                            </p>
                        </div>
                        <button onClick={manualCheck} className="border px-2 py-1 rounded text-xs hover:bg-white">
                            Check now
                        </button>
                    </div>
                    <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
                        {(["stable", "dev"] as const).map((c) => {
                            const ch = releases.data?.channels?.[c];
                            return (
                                <div key={c} className="bg-white border rounded p-2">
                                    <div className="font-medium text-slate-700 capitalize">{c}</div>
                                    {ch ? (
                                        <>
                                            <div className="font-mono">{ch.tag}</div>
                                            <div className="text-slate-400 truncate">{ch.image}</div>
                                            {ch.published_at && (
                                                <div className="text-slate-400">
                                                    {new Date(ch.published_at).toLocaleString()}
                                                </div>
                                            )}
                                        </>
                                    ) : (
                                        <div className="text-slate-400">(no release detected)</div>
                                    )}
                                </div>
                            );
                        })}
                    </div>
                    {releases.data.last_checked_at && (
                        <p className="text-slate-400 text-xs mt-2">
                            Last checked {new Date(releases.data.last_checked_at).toLocaleString()}
                            {releases.data.last_error && (
                                <span className="text-red-600"> · {releases.data.last_error}</span>
                            )}
                        </p>
                    )}
                </div>
            )}

            <div className="flex justify-end mb-3">
                <button
                    onClick={() => setCreating(true)}
                    className="bg-blue-600 hover:bg-blue-700 text-white px-3 py-1.5 rounded text-sm"
                >
                    + Add profile
                </button>
            </div>

            {isLoading && <p className="text-slate-400 text-sm">Loading…</p>}
            {applyMsg && <pre className="bg-slate-100 text-xs p-2 rounded mb-3 whitespace-pre-wrap">{applyMsg}</pre>}

            <table className="w-full text-sm border rounded overflow-hidden">
                <thead className="bg-slate-50 text-xs uppercase text-slate-500">
                    <tr>
                        <th className="text-left px-3 py-2">Name</th>
                        <th className="text-left px-3 py-2">Env</th>
                        <th className="text-left px-3 py-2">Image</th>
                        <th className="text-left px-3 py-2">AWS</th>
                        <th className="text-left px-3 py-2">Channel</th>
                        <th className="text-left px-3 py-2">Secrets</th>
                        <th className="text-right px-3 py-2">Actions</th>
                    </tr>
                </thead>
                <tbody>
                    {data?.data?.map((p) => {
                        const awsCred = aws.data?.data?.find((a) => a.id === p.aws_credential_id);
                        return (
                            <tr key={p.id} className="border-t">
                                <td className="px-3 py-2">
                                    <div className="font-medium">{p.name}</div>
                                    <div className="text-slate-400 text-xs">{p.description}</div>
                                </td>
                                <td className="px-3 py-2 text-slate-600">{p.app_env}</td>
                                <td className="px-3 py-2 font-mono text-xs">{p.worker_image}</td>
                                <td className="px-3 py-2 text-slate-600">{awsCred?.name ?? "—"}</td>
                                <td className="px-3 py-2 text-xs">
                                    <div className="text-slate-600">{p.release_channel}</div>
                                    {p.resolved_image_tag && (
                                        <div className="text-slate-400 font-mono">{p.resolved_image_tag}</div>
                                    )}
                                    {p.auto_update && p.release_channel !== "pinned" && (
                                        <div className="text-green-600">auto</div>
                                    )}
                                </td>
                                <td className="px-3 py-2 text-xs text-slate-500">
                                    {p.has_kafka_password ? "kafka " : ""}
                                    {p.has_schema_secret ? "schema " : ""}
                                    {p.has_redis_url ? "redis " : ""}
                                </td>
                                <td className="px-3 py-2 text-right">
                                    <button onClick={() => applyAll(p)} className="text-amber-700 text-sm hover:underline mr-3">Apply</button>
                                    <button onClick={() => setEditing(p)} className="text-blue-600 text-sm hover:underline mr-3">Edit</button>
                                    <button
                                        onClick={async () => {
                                            if (!confirm(`Delete ${p.name}?`)) return;
                                            try {
                                                await deleteWorkerProfile(p.id);
                                                qc.invalidateQueries({ queryKey: ["admin", "profiles"] });
                                            } catch (e: any) {
                                                alert(e?.message || "delete failed");
                                            }
                                        }}
                                        className="text-red-600 text-sm hover:underline"
                                    >
                                        Delete
                                    </button>
                                </td>
                            </tr>
                        );
                    })}
                    {data && data.data?.length === 0 && (
                        <tr><td colSpan={7} className="text-center text-slate-400 py-8 text-sm">No profiles yet.</td></tr>
                    )}
                </tbody>
            </table>

            {(creating || editing) && (
                <ProfileForm
                    initial={editing}
                    awsOptions={aws.data?.data ?? []}
                    onClose={() => { setEditing(null); setCreating(false); }}
                    onSaved={() => {
                        setEditing(null);
                        setCreating(false);
                        qc.invalidateQueries({ queryKey: ["admin", "profiles"] });
                    }}
                />
            )}
        </div>
    );
}

function ProfileForm({
    initial,
    awsOptions,
    onClose,
    onSaved,
}: {
    initial: WorkerProfile | null;
    awsOptions: AWSCredentials[];
    onClose: () => void;
    onSaved: () => void;
}) {
    const [form, setForm] = useState<WorkerProfileBody>({
        name: initial?.name ?? "",
        description: initial?.description ?? "",
        app_env: initial?.app_env ?? "prod",
        worker_image: initial?.worker_image ?? "ghcr.io/warmbly/worker:latest",
        kafka_bootstrap_servers: initial?.kafka_bootstrap_servers ?? "",
        kafka_sasl_username: initial?.kafka_sasl_username ?? "",
        kafka_sasl_password: "",
        schema_registry_url: initial?.schema_registry_url ?? "",
        schema_registry_key: initial?.schema_registry_key ?? "",
        schema_registry_secret: "",
        redis_url: "",
        aws_credential_id: initial?.aws_credential_id ?? null,
    });
    const [channel, setChannel] = useState<"pinned" | "stable" | "dev">(initial?.release_channel ?? "pinned");
    const [autoUpdate, setAutoUpdate] = useState<boolean>(initial?.auto_update ?? false);

    const mut = useMutation({
        mutationFn: async () => {
            const res = initial
                ? await updateWorkerProfile(initial.id, form)
                : await createWorkerProfile(form);
            const id = initial ? initial.id : (res as { id: string }).id;
            // Persist release settings alongside the profile save.
            await setProfileRelease(id, channel, autoUpdate);
            return res;
        },
        onSuccess: onSaved,
    });

    return (
        <Modal title={initial ? `Edit ${initial.name}` : "Add worker profile"} onClose={onClose} wide>
            <Field label="Name">
                <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} className={inp} required />
            </Field>
            <Field label="Description">
                <input value={form.description ?? ""} onChange={(e) => setForm({ ...form, description: e.target.value })} className={inp} />
            </Field>

            <div className="grid grid-cols-2 gap-3">
                <Field label="App env">
                    <select value={form.app_env} onChange={(e) => setForm({ ...form, app_env: e.target.value })} className={inp}>
                        <option value="prod">prod</option>
                        <option value="dev">dev</option>
                    </select>
                </Field>
                <Field label="Worker image">
                    <input value={form.worker_image} onChange={(e) => setForm({ ...form, worker_image: e.target.value })} className={inp} />
                </Field>
            </div>

            <Field label="AWS credentials">
                <select
                    value={form.aws_credential_id ?? ""}
                    onChange={(e) => setForm({ ...form, aws_credential_id: e.target.value || null })}
                    className={inp}
                >
                    <option value="">(none)</option>
                    {awsOptions.map((a) => (
                        <option key={a.id} value={a.id}>{a.name} ({a.region})</option>
                    ))}
                </select>
            </Field>

            <h4 className="font-semibold text-slate-600 text-sm mt-4 mb-1">Kafka</h4>
            <Field label="Bootstrap servers">
                <input value={form.kafka_bootstrap_servers} onChange={(e) => setForm({ ...form, kafka_bootstrap_servers: e.target.value })} className={inp} />
            </Field>
            <div className="grid grid-cols-2 gap-3">
                <Field label="SASL username">
                    <input value={form.kafka_sasl_username} onChange={(e) => setForm({ ...form, kafka_sasl_username: e.target.value })} className={inp} />
                </Field>
                <Field label="SASL password" hint={initial?.has_kafka_password ? "Stored. Leave blank to keep." : "Encrypted at rest."}>
                    <input
                        type="password"
                        value={form.kafka_sasl_password ?? ""}
                        onChange={(e) => setForm({ ...form, kafka_sasl_password: e.target.value })}
                        placeholder={initial?.has_kafka_password ? "••••••" : ""}
                        className={inp}
                    />
                </Field>
            </div>

            <h4 className="font-semibold text-slate-600 text-sm mt-4 mb-1">Schema registry</h4>
            <Field label="URL">
                <input value={form.schema_registry_url} onChange={(e) => setForm({ ...form, schema_registry_url: e.target.value })} className={inp} />
            </Field>
            <div className="grid grid-cols-2 gap-3">
                <Field label="Key">
                    <input value={form.schema_registry_key} onChange={(e) => setForm({ ...form, schema_registry_key: e.target.value })} className={inp} />
                </Field>
                <Field label="Secret" hint={initial?.has_schema_secret ? "Stored. Leave blank to keep." : "Encrypted at rest."}>
                    <input
                        type="password"
                        value={form.schema_registry_secret ?? ""}
                        onChange={(e) => setForm({ ...form, schema_registry_secret: e.target.value })}
                        placeholder={initial?.has_schema_secret ? "••••••" : ""}
                        className={inp}
                    />
                </Field>
            </div>

            <h4 className="font-semibold text-slate-600 text-sm mt-4 mb-1">Redis</h4>
            <Field label="URL" hint={initial?.has_redis_url ? "Stored. Leave blank to keep." : "Encrypted in full (contains password)."}>
                <input
                    type="password"
                    value={form.redis_url ?? ""}
                    onChange={(e) => setForm({ ...form, redis_url: e.target.value })}
                    placeholder={initial?.has_redis_url ? "redis://••••••" : "redis://user:pass@host:6379"}
                    className={inp}
                />
            </Field>

            <h4 className="font-semibold text-slate-600 text-sm mt-4 mb-1">Release channel</h4>
            <div className="flex gap-3 mb-2">
                {(["pinned", "stable", "dev"] as const).map((c) => (
                    <label key={c} className="flex items-center gap-1.5 text-sm text-slate-600">
                        <input
                            type="radio"
                            name="channel"
                            value={c}
                            checked={channel === c}
                            onChange={() => setChannel(c)}
                        />
                        {c}
                    </label>
                ))}
            </div>
            <p className="text-slate-400 text-xs mb-2">
                {channel === "pinned" && "Image is whatever you set above. Auto-update disabled."}
                {channel === "stable" && "Backend tracks the latest non-prerelease GitHub release."}
                {channel === "dev" && "Backend tracks the latest release including prereleases."}
            </p>
            <label className={`flex items-center gap-2 text-sm ${channel === "pinned" ? "opacity-50" : ""}`}>
                <input
                    type="checkbox"
                    checked={autoUpdate}
                    disabled={channel === "pinned"}
                    onChange={(e) => setAutoUpdate(e.target.checked)}
                />
                <span>Auto-roll new versions to all assigned workers</span>
            </label>
            <p className="text-slate-400 text-xs mt-1">
                Off: backend records new versions, dashboard shows "update available", you click Apply.
                On: workers update automatically when a new release fires the webhook.
            </p>

            {mut.isError && <p className="text-red-600 text-sm mt-3">{(mut.error as any)?.message ?? "failed"}</p>}

            <div className="flex justify-end gap-2 mt-4">
                <button onClick={onClose} className="border px-3 py-1.5 rounded text-sm">Cancel</button>
                <button
                    onClick={() => mut.mutate()}
                    disabled={mut.isPending}
                    className="bg-blue-600 hover:bg-blue-700 text-white px-3 py-1.5 rounded text-sm disabled:opacity-50"
                >
                    {mut.isPending ? "Saving…" : "Save"}
                </button>
            </div>
            <p className="text-slate-400 text-xs mt-3">
                Saving doesn't restart any worker. Use the Apply button to push these
                values to all workers assigned to this profile.
            </p>
        </Modal>
    );
}

// shared bits

const inp = "w-full border rounded px-3 py-2 text-sm";

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
    return (
        <div className="mb-2">
            <label className="text-xs font-medium text-slate-500 uppercase block mb-1">{label}</label>
            {children}
            {hint && <p className="text-slate-400 text-xs mt-0.5">{hint}</p>}
        </div>
    );
}

function Modal({
    title,
    children,
    onClose,
    wide,
}: {
    title: string;
    children: React.ReactNode;
    onClose: () => void;
    wide?: boolean;
}) {
    return (
        <div className="fixed inset-0 bg-black/40 flex items-start justify-center pt-12 z-50" onClick={onClose}>
            <div
                className={`bg-white rounded-lg shadow-xl p-6 w-full ${wide ? "max-w-2xl" : "max-w-lg"} max-h-[90vh] overflow-auto`}
                onClick={(e) => e.stopPropagation()}
            >
                <h3 className="text-slate-700 font-semibold text-base mb-4">{title}</h3>
                {children}
            </div>
        </div>
    );
}
