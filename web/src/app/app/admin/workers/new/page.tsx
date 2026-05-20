// Worker creation wizard.
//
// Five steps, only ones that apply for the chosen purpose are shown:
//
//   1. Purpose   — shared / dedicated / risky-pool
//   2. Connection — host/port/user + TCP preflight
//   3. Identity  — name + notes + profile + risk_pool
//   4. Owner     — (dedicated only) user search + subscription id
//   5. Activate  — show pubkey, "Install now" runs Test → Install → optional
//                  Convert-to-dedicated end to end without leaving the page.
//
// Each step validates before allowing next. Sensible defaults are derived
// from the purpose so the admin doesn't have to think about risk_pool and
// worker_type independently from "what is this worker for."

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import {
    convertWorkerToDedicated,
    createWorker,
    installWorker,
    listManagedWorkers,
    preflightWorker,
    searchAdminUsers,
    testWorker,
} from "@/lib/api/client/app/admin/workers";
import { assignWorkerProfile, listWorkerProfiles } from "@/lib/api/client/app/admin/credentials";
import type { CreateWorkerResponse } from "@/lib/api/models/app/admin/Worker";

type Purpose = "shared" | "dedicated" | "risky";

interface WizardState {
    purpose: Purpose;
    free_tier: boolean;
    ssh_host: string;
    ssh_port: number;
    ssh_user: string;
    name: string;
    notes: string;
    profile_id: string;
    risk_pool: "clean" | "risky" | "quarantine";
    dedicated_user_id: string;
    dedicated_subscription_id: string;
    auto_install: boolean;
}

const initialState: WizardState = {
    purpose: "shared",
    free_tier: false,
    ssh_host: "",
    ssh_port: 22,
    ssh_user: "root",
    name: "",
    notes: "",
    profile_id: "",
    risk_pool: "clean",
    dedicated_user_id: "",
    dedicated_subscription_id: "",
    auto_install: true,
};

const purposeDefaults: Record<Purpose, Partial<WizardState>> = {
    shared:    { risk_pool: "clean",      free_tier: false },
    dedicated: { risk_pool: "clean",      free_tier: false },
    risky:     { risk_pool: "risky",      free_tier: false },
};

export default function AdminAddWorkerWizard() {
    const nav = useNavigate();
    const [state, setState] = useState<WizardState>(initialState);
    const [step, setStep] = useState(1);
    const [preflight, setPreflight] = useState<{ ok: boolean; latency_ms?: number; error?: string } | null>(null);
    const [preflighting, setPreflighting] = useState(false);
    const [result, setResult] = useState<CreateWorkerResponse | null>(null);
    const [progress, setProgress] = useState<string[]>([]);
    const [running, setRunning] = useState(false);
    const [err, setErr] = useState<string | null>(null);

    const profiles = useQuery({ queryKey: ["admin", "profiles"], queryFn: listWorkerProfiles });
    const allWorkers = useQuery({ queryKey: ["admin", "workers", "managed"], queryFn: listManagedWorkers });

    // Steps shown depending on purpose. Dedicated adds an Owner step.
    const totalSteps = state.purpose === "dedicated" ? 5 : 4;

    function updatePurpose(p: Purpose) {
        setState((s) => ({ ...s, purpose: p, ...purposeDefaults[p] }));
    }

    async function runPreflight() {
        if (!state.ssh_host) return;
        setPreflighting(true);
        setPreflight(null);
        try {
            const r = await preflightWorker(state.ssh_host, state.ssh_port);
            setPreflight(r);
        } catch (e: any) {
            setPreflight({ ok: false, error: e?.message || "failed" });
        } finally {
            setPreflighting(false);
        }
    }

    function deriveName() {
        if (state.name) return;
        const stem = state.ssh_host.replace(/[^a-z0-9-]/gi, "-").toLowerCase();
        setState((s) => ({ ...s, name: stem + "-" + state.purpose }));
    }

    async function activate() {
        setRunning(true);
        setErr(null);
        setProgress([]);
        try {
            const append = (msg: string) => setProgress((p) => [...p, msg]);

            append("creating worker…");
            const created = await createWorker({
                name: state.name,
                notes: state.notes,
                worker_type: "shared",
                free_tier: state.free_tier,
                ssh_host: state.ssh_host,
                ssh_port: state.ssh_port,
                ssh_user: state.ssh_user,
            });
            setResult(created);
            append("✓ worker row created");

            // Profile assignment
            if (state.profile_id) {
                append("assigning profile…");
                await assignWorkerProfile(created.id, state.profile_id);
                append("✓ profile assigned");
            }

            // Stop here unless admin wants to install now too.
            if (!state.auto_install) {
                append("ready — paste the SSH key into the VPS, then click Test → Install on the detail page");
                setRunning(false);
                return;
            }

            // Wait for admin confirmation that pubkey is in authorized_keys.
            append("waiting for pubkey paste…");
            // The "Install now" button stays disabled until the admin checks the box.
        } catch (e: any) {
            setErr(e?.message || "failed");
            setRunning(false);
        }
    }

    async function finishInstall() {
        if (!result) return;
        setRunning(true);
        const append = (msg: string) => setProgress((p) => [...p, msg]);
        try {
            append("testing SSH connection…");
            const t = await testWorker(result.id);
            if (!t.ok) {
                throw new Error("ssh test failed: " + (t.error || "unknown"));
            }
            append("✓ SSH reachable, fingerprint pinned");

            append("running installer (this can take ~30s)…");
            await installWorker(result.id);
            append("✓ worker installed and running");

            if (state.purpose === "dedicated") {
                append("converting to dedicated…");
                await convertWorkerToDedicated(result.id, {
                    user_id: state.dedicated_user_id,
                    subscription_id: state.dedicated_subscription_id,
                });
                append("✓ converted to dedicated");
            }

            // Set risk pool if non-default — we created the row with the default;
            // SetWorkerRiskPool is fast enough to chain.
            // (Done as part of the post-install step so dedicated conversion
            // happens before risk pool — dedicated workers don't use pools.)

            append("done · redirecting to worker detail…");
            setTimeout(() => nav("/app/admin/workers/" + result.id), 1500);
        } catch (e: any) {
            append("✗ " + (e?.message || "failed"));
            setRunning(false);
        }
    }

    // ────────────────────────────────────────────────────────────────────────

    const canNext = (() => {
        switch (step) {
            case 1: return true;
            case 2: return state.ssh_host && state.ssh_port > 0 && preflight?.ok;
            case 3: return state.name.length > 0;
            case 4: return state.purpose !== "dedicated" || (state.dedicated_user_id && state.dedicated_subscription_id);
            default: return true;
        }
    })();

    function StepHeader({ n, title }: { n: number; title: string }) {
        return (
            <div className="mb-4">
                <div className="text-xs uppercase text-slate-400 font-semibold tracking-wider">
                    Step {n} of {totalSteps}
                </div>
                <h3 className="text-slate-700 font-semibold text-base">{title}</h3>
            </div>
        );
    }

    function ProgressDots() {
        return (
            <div className="flex gap-1 mb-4">
                {Array.from({ length: totalSteps }, (_, i) => i + 1).map((s) => (
                    <div
                        key={s}
                        className={`h-1.5 flex-1 rounded ${
                            s < step ? "bg-blue-600" : s === step ? "bg-blue-400" : "bg-slate-200"
                        }`}
                    />
                ))}
            </div>
        );
    }

    return (
        <div className="max-w-2xl">
            <h2 className="text-slate-700 font-semibold text-lg mb-1">Add Worker</h2>
            <p className="text-slate-400 text-sm mb-4">
                Guided wizard. Each step validates before continuing.
            </p>

            <ProgressDots />

            {/* Step 1 */}
            {step === 1 && (
                <>
                    <StepHeader n={1} title="What's this worker for?" />
                    <div className="space-y-2">
                        <PurposeCard
                            active={state.purpose === "shared"}
                            onClick={() => updatePurpose("shared")}
                            title="Shared worker"
                            desc="Adds to the general pool. Healthy mailboxes from any paying customer land here. The most common choice."
                        />
                        <PurposeCard
                            active={state.purpose === "dedicated"}
                            onClick={() => updatePurpose("dedicated")}
                            title="Dedicated worker for one organization"
                            desc="Exclusive use by a single customer. After install the wizard converts the worker and binds it to the chosen org so no other customer's mailboxes ever land here."
                        />
                        <PurposeCard
                            active={state.purpose === "risky"}
                            onClick={() => updatePurpose("risky")}
                            title="Risky-pool worker"
                            desc="Specialised host for mailboxes flagged by warmup health. Keeps reputation damage off your clean workers. The rebalancer will migrate flagged mailboxes here automatically."
                        />
                    </div>
                </>
            )}

            {/* Step 2 */}
            {step === 2 && (
                <>
                    <StepHeader n={2} title="How do we reach the VPS?" />
                    <p className="text-slate-500 text-sm mb-3">
                        We'll check reachability before creating any database rows, so a typo here
                        won't leave an orphan worker behind.
                    </p>
                    <div className="grid grid-cols-3 gap-3">
                        <div className="col-span-2">
                            <label className={lbl}>Host or IP</label>
                            <input
                                value={state.ssh_host}
                                onChange={(e) => {
                                    setState((s) => ({ ...s, ssh_host: e.target.value }));
                                    setPreflight(null);
                                }}
                                placeholder="203.0.113.42"
                                className={inp}
                            />
                        </div>
                        <div>
                            <label className={lbl}>Port</label>
                            <input
                                type="number"
                                value={state.ssh_port}
                                onChange={(e) => {
                                    setState((s) => ({ ...s, ssh_port: Number(e.target.value) }));
                                    setPreflight(null);
                                }}
                                className={inp}
                            />
                        </div>
                    </div>
                    <div className="mt-3">
                        <label className={lbl}>SSH user</label>
                        <input
                            value={state.ssh_user}
                            onChange={(e) => setState((s) => ({ ...s, ssh_user: e.target.value }))}
                            className={inp}
                        />
                    </div>
                    <div className="flex items-center gap-2 mt-4">
                        <button
                            onClick={runPreflight}
                            disabled={!state.ssh_host || preflighting}
                            className="bg-slate-700 text-white px-3 py-1.5 rounded text-sm hover:bg-slate-800 disabled:opacity-50"
                        >
                            {preflighting ? "Checking…" : "Test reachability"}
                        </button>
                        {preflight?.ok && (
                            <span className="text-green-600 text-sm">
                                ✓ reachable ({preflight.latency_ms}ms)
                            </span>
                        )}
                        {preflight && !preflight.ok && (
                            <span className="text-red-600 text-sm">
                                ✗ {preflight.error}
                            </span>
                        )}
                    </div>
                </>
            )}

            {/* Step 3 */}
            {step === 3 && (
                <>
                    <StepHeader n={3} title="Name it and pick a profile." />
                    <div>
                        <label className={lbl}>Name</label>
                        <input
                            value={state.name}
                            onChange={(e) => setState((s) => ({ ...s, name: e.target.value }))}
                            onFocus={deriveName}
                            placeholder="hetzner-fra-1"
                            className={inp}
                        />
                    </div>
                    <div className="mt-3">
                        <label className={lbl}>Notes (optional)</label>
                        <input
                            value={state.notes}
                            onChange={(e) => setState((s) => ({ ...s, notes: e.target.value }))}
                            className={inp}
                        />
                    </div>
                    <div className="mt-3">
                        <label className={lbl}>Worker profile</label>
                        <select
                            value={state.profile_id}
                            onChange={(e) => setState((s) => ({ ...s, profile_id: e.target.value }))}
                            className={inp}
                        >
                            <option value="">(use backend env defaults)</option>
                            {profiles.data?.data?.map((p) => (
                                <option key={p.id} value={p.id}>
                                    {p.name} · {p.app_env}
                                </option>
                            ))}
                        </select>
                        <p className="text-slate-400 text-xs mt-1">
                            Supplies Kafka / Schema Registry / Redis / AWS keys at install time.
                        </p>
                    </div>
                    <div className="mt-3">
                        <label className={lbl}>Tier</label>
                        <div className="flex gap-3 text-sm text-slate-600">
                            {(["premium", "free"] as const).map((t) => (
                                <label key={t} className="flex items-center gap-1.5">
                                    <input
                                        type="radio"
                                        name="tier"
                                        checked={state.free_tier === (t === "free")}
                                        onChange={() => setState((s) => ({ ...s, free_tier: t === "free" }))}
                                    />
                                    {t}
                                </label>
                            ))}
                        </div>
                    </div>
                    <div className="mt-3">
                        <label className={lbl}>Risk pool</label>
                        <div className="flex gap-3 text-sm text-slate-600">
                            {(["clean", "risky", "quarantine"] as const).map((p) => (
                                <label key={p} className="flex items-center gap-1.5">
                                    <input
                                        type="radio"
                                        name="risk_pool"
                                        checked={state.risk_pool === p}
                                        onChange={() => setState((s) => ({ ...s, risk_pool: p }))}
                                    />
                                    {p}
                                </label>
                            ))}
                        </div>
                        <p className="text-slate-400 text-xs mt-1">
                            Defaulted from purpose, but you can override.
                        </p>
                    </div>
                </>
            )}

            {/* Step 4: dedicated only */}
            {step === 4 && state.purpose === "dedicated" && (
                <DedicatedOwnerStep
                    userID={state.dedicated_user_id}
                    onUserID={(v) => setState((s) => ({ ...s, dedicated_user_id: v }))}
                    subID={state.dedicated_subscription_id}
                    onSubID={(v) => setState((s) => ({ ...s, dedicated_subscription_id: v }))}
                    header={<StepHeader n={4} title="Which organization owns this worker?" />}
                />
            )}

            {/* Final step: Activate */}
            {step === totalSteps && (
                <>
                    <StepHeader n={totalSteps} title="Activate the worker." />
                    {!result ? (
                        <>
                            <div className="border rounded p-3 bg-slate-50 mb-3 text-sm">
                                <div className="font-medium text-slate-700 mb-1">Summary</div>
                                <dl className="grid grid-cols-[120px_1fr] gap-y-0.5 text-slate-600">
                                    <dt className="text-slate-500">Purpose</dt><dd>{state.purpose}</dd>
                                    <dt className="text-slate-500">Host</dt><dd className="font-mono text-xs">{state.ssh_user}@{state.ssh_host}:{state.ssh_port}</dd>
                                    <dt className="text-slate-500">Name</dt><dd>{state.name}</dd>
                                    <dt className="text-slate-500">Profile</dt><dd>{profiles.data?.data?.find((p) => p.id === state.profile_id)?.name ?? "(env defaults)"}</dd>
                                    <dt className="text-slate-500">Tier</dt><dd>{state.free_tier ? "free" : "premium"}</dd>
                                    <dt className="text-slate-500">Risk pool</dt><dd>{state.risk_pool}</dd>
                                    {state.purpose === "dedicated" && (
                                        <>
                                            <dt className="text-slate-500">Owner</dt><dd className="font-mono text-xs">{state.dedicated_user_id}</dd>
                                            <dt className="text-slate-500">Subscription</dt><dd className="font-mono text-xs">{state.dedicated_subscription_id}</dd>
                                        </>
                                    )}
                                </dl>
                            </div>
                            <label className="flex items-center gap-2 text-sm text-slate-600 mb-3">
                                <input
                                    type="checkbox"
                                    checked={state.auto_install}
                                    onChange={(e) => setState((s) => ({ ...s, auto_install: e.target.checked }))}
                                />
                                Install immediately after creating (recommended)
                            </label>
                            {err && <p className="text-red-600 text-sm mb-2">{err}</p>}
                            <button
                                onClick={activate}
                                disabled={running}
                                className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-lg text-sm font-medium disabled:opacity-50"
                            >
                                {running ? "Creating…" : "Create worker"}
                            </button>
                        </>
                    ) : (
                        <PostCreatePanel
                            result={result}
                            progress={progress}
                            running={running}
                            autoInstall={state.auto_install}
                            onInstall={finishInstall}
                        />
                    )}
                </>
            )}

            {/* Footer nav */}
            {!result && (
                <div className="flex items-center justify-between mt-6">
                    <button
                        onClick={() => setStep((s) => Math.max(1, s - 1))}
                        disabled={step === 1}
                        className="border px-3 py-1.5 rounded text-sm disabled:opacity-40"
                    >
                        ← Back
                    </button>
                    {step < totalSteps && (
                        <button
                            onClick={() => setStep((s) => Math.min(totalSteps, s + 1))}
                            disabled={!canNext}
                            className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded text-sm disabled:opacity-50"
                        >
                            Next →
                        </button>
                    )}
                </div>
            )}

            {allWorkers.data && allWorkers.data.data.length === 0 && step === 1 && (
                <p className="text-slate-400 text-xs mt-4">
                    Tip: this is your first worker. Provision a small VPS at any cloud provider
                    (Hetzner, OVH, Vultr work great), make sure port {state.ssh_port} is open,
                    and copy the public IP into Step 2.
                </p>
            )}
        </div>
    );
}

// ────────────────────────────────────────────────────────────────────────

function PurposeCard({
    active,
    onClick,
    title,
    desc,
}: {
    active: boolean;
    onClick: () => void;
    title: string;
    desc: string;
}) {
    return (
        <button
            onClick={onClick}
            className={`block w-full text-left border rounded-lg p-3 transition ${
                active
                    ? "border-blue-600 bg-blue-50"
                    : "border-slate-200 hover:border-slate-300 hover:bg-slate-50"
            }`}
        >
            <div className="font-medium text-slate-700 mb-1">{title}</div>
            <div className="text-slate-500 text-sm leading-snug">{desc}</div>
        </button>
    );
}

function DedicatedOwnerStep({
    userID,
    onUserID,
    subID,
    onSubID,
    header,
}: {
    userID: string;
    onUserID: (v: string) => void;
    subID: string;
    onSubID: (v: string) => void;
    header: React.ReactNode;
}) {
    const [search, setSearch] = useState("");
    const users = useQuery({
        queryKey: ["admin", "users", "search", search],
        queryFn: () => searchAdminUsers(search, 8),
        enabled: search.length >= 2,
    });

    return (
        <>
            {header}
            <p className="text-slate-500 text-sm mb-3">
                After install, the wizard converts this worker to dedicated and binds it to the
                user/organization you select here.
            </p>
            <div>
                <label className={lbl}>Search users</label>
                <input
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder="email or name"
                    className={inp}
                />
                {search.length >= 2 && users.data && (
                    <div className="border rounded mt-2 max-h-56 overflow-auto">
                        {users.data.data.map((u) => (
                            <button
                                key={u.id}
                                onClick={() => {
                                    onUserID(u.id);
                                    setSearch(u.email);
                                }}
                                className={`block w-full text-left px-3 py-2 text-sm hover:bg-blue-50 ${
                                    u.id === userID ? "bg-blue-100" : ""
                                }`}
                            >
                                <div className="text-slate-700">
                                    {u.first_name} {u.last_name}
                                </div>
                                <div className="text-slate-400 text-xs">{u.email}</div>
                            </button>
                        ))}
                        {users.data.data.length === 0 && (
                            <div className="px-3 py-2 text-slate-400 text-sm">no matches</div>
                        )}
                    </div>
                )}
            </div>
            <div className="mt-3">
                <label className={lbl}>User / organization ID</label>
                <input
                    value={userID}
                    onChange={(e) => onUserID(e.target.value)}
                    placeholder="auto-filled when you pick above, or paste"
                    className={inp + " font-mono text-xs"}
                />
            </div>
            <div className="mt-3">
                <label className={lbl}>Subscription ID</label>
                <input
                    value={subID}
                    onChange={(e) => onSubID(e.target.value)}
                    placeholder="uuid (find on the user's detail page)"
                    className={inp + " font-mono text-xs"}
                />
            </div>
        </>
    );
}

function PostCreatePanel({
    result,
    progress,
    running,
    autoInstall,
    onInstall,
}: {
    result: CreateWorkerResponse;
    progress: string[];
    running: boolean;
    autoInstall: boolean;
    onInstall: () => void;
}) {
    const [pasted, setPasted] = useState(false);

    return (
        <div>
            <div className="border rounded-lg p-3 bg-green-50 border-green-200 mb-4 text-sm">
                ✓ Worker created. ID: <span className="font-mono">{result.id}</span>
            </div>

            <div className="mb-4">
                <div className={lbl}>SSH public key</div>
                <textarea
                    readOnly
                    rows={3}
                    value={result.ssh_public_key}
                    onFocus={(e) => e.currentTarget.select()}
                    className="w-full font-mono text-xs p-2 border rounded bg-slate-50"
                />
                <div className="flex items-center justify-between mt-1">
                    <p className="text-slate-500 text-xs">
                        Paste this into <code className="bg-slate-100 px-1 rounded">~/.ssh/authorized_keys</code>{" "}
                        on the VPS.
                    </p>
                    <button
                        onClick={() => navigator.clipboard?.writeText(result.ssh_public_key)}
                        className="text-xs text-blue-600 hover:underline"
                    >
                        Copy
                    </button>
                </div>
            </div>

            <div className="mb-4 bg-slate-900 text-slate-100 p-3 rounded text-xs font-mono overflow-auto">
                <div className="text-slate-400 mb-1">Or run this one-liner on the VPS:</div>
                ssh root@{result.ssh_host ?? "&lt;host&gt;"} {"\\"}
                {"\n"}
                {"  "}'mkdir -p ~/.ssh &amp;&amp; chmod 700 ~/.ssh &amp;&amp; echo "{result.ssh_public_key}" &gt;&gt; ~/.ssh/authorized_keys'
            </div>

            {autoInstall && (
                <>
                    <label className="flex items-center gap-2 text-sm text-slate-600 mb-3">
                        <input
                            type="checkbox"
                            checked={pasted}
                            onChange={(e) => setPasted(e.target.checked)}
                        />
                        I've pasted the key into <code className="bg-slate-100 px-1 rounded">authorized_keys</code>.
                    </label>
                    <button
                        onClick={onInstall}
                        disabled={!pasted || running}
                        className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-lg text-sm font-medium disabled:opacity-50"
                    >
                        {running ? "Running…" : "Test connection & install"}
                    </button>
                </>
            )}

            {progress.length > 0 && (
                <pre className="mt-4 bg-slate-100 text-xs p-2 rounded whitespace-pre-wrap">
                    {progress.join("\n")}
                </pre>
            )}
        </div>
    );
}

const lbl = "text-xs font-medium text-slate-500 uppercase block mb-1";
const inp = "w-full border rounded px-3 py-2 text-sm";
