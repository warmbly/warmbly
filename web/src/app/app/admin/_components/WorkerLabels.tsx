// Worker labels: shared helpers for the auto-derived "smart" labels
// (tier:free, pool:risky, state:error, version:vX.Y.Z, ...) and the
// rendering primitives that the list + detail pages use.
//
// The auto labels are computed client-side from the worker row so they
// always match the source of truth without a separate sync step. Admin
// tags live in workers.tags and are edited via the tag editor below.

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { listAllWorkerTags, setWorkerTags } from "@/lib/api/client/app/admin/workers";
import type { ManagedWorker } from "@/lib/api/models/app/admin/Worker";

const OFFLINE_MS = 5 * 60_000;

export function smartLabels(w: ManagedWorker): string[] {
    const labels: string[] = [];
    labels.push(`type:${w.worker_type}`);
    if (w.worker_type === "shared") {
        labels.push(`tier:${w.free_tier ? "free" : "premium"}`);
        labels.push(`pool:${w.risk_pool}`);
    }
    labels.push(`state:${w.install_state}`);
    if (w.image_version) labels.push(`ver:${w.image_version}`);
    // Liveness — only meaningful for installed workers.
    if (w.install_state === "installed") {
        const age = w.last_seen_at ? Date.now() - new Date(w.last_seen_at).getTime() : Infinity;
        if (age > OFFLINE_MS) labels.push("liveness:offline");
        else if (age > 90_000) labels.push("liveness:stale");
        else labels.push("liveness:online");
    }
    return labels;
}

const TONE_BY_PREFIX: Record<string, string> = {
    "state:error":        "bg-red-100    text-red-700",
    "liveness:offline":   "bg-red-100    text-red-700",
    "liveness:stale":     "bg-amber-100  text-amber-700",
    "liveness:online":    "bg-green-100  text-green-700",
    "pool:risky":         "bg-amber-100  text-amber-700",
    "pool:quarantine":    "bg-red-100    text-red-700",
    "pool:clean":         "bg-slate-100  text-slate-600",
    "tier:free":          "bg-purple-100 text-purple-700",
    "tier:premium":       "bg-indigo-100 text-indigo-700",
    "type:dedicated":     "bg-blue-100   text-blue-700",
    "type:shared":        "bg-slate-100  text-slate-600",
};

function smartTone(label: string): string {
    if (TONE_BY_PREFIX[label]) return TONE_BY_PREFIX[label];
    if (label.startsWith("ver:")) return "bg-slate-100 text-slate-500 font-mono";
    if (label.startsWith("state:")) return "bg-slate-100 text-slate-600";
    return "bg-slate-100 text-slate-600";
}

export function SmartLabel({ label }: { label: string }) {
    return (
        <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium ${smartTone(label)}`}>
            {label}
        </span>
    );
}

export function UserTag({ tag, onRemove }: { tag: string; onRemove?: () => void }) {
    return (
        <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-blue-50 text-blue-700 text-[11px] border border-blue-200">
            {tag}
            {onRemove && (
                <button onClick={onRemove} className="text-blue-500 hover:text-blue-700" aria-label="remove">
                    ×
                </button>
            )}
        </span>
    );
}

export function TagsCell({ worker, max = 4 }: { worker: ManagedWorker; max?: number }) {
    const auto = smartLabels(worker);
    const user = worker.tags ?? [];
    // Show user tags first (admin intent), then a couple of high-signal smart
    // labels. Saturate at `max` total to keep the row tidy.
    const shown = [...user];
    const importantAuto = auto.filter((l) =>
        l.startsWith("liveness:offline") ||
        l.startsWith("state:error") ||
        l.startsWith("pool:risky") ||
        l.startsWith("pool:quarantine"),
    );
    for (const a of importantAuto) {
        if (shown.length >= max) break;
        shown.push(a);
    }
    return (
        <div className="flex flex-wrap gap-1">
            {shown.map((t) =>
                user.includes(t) ? <UserTag key={t} tag={t} /> : <SmartLabel key={t} label={t} />,
            )}
            {user.length + importantAuto.length > shown.length && (
                <span className="text-slate-400 text-[10px] px-1">+{user.length + importantAuto.length - shown.length}</span>
            )}
        </div>
    );
}

export function TagEditor({
    worker,
    onSaved,
}: {
    worker: ManagedWorker;
    onSaved?: (tags: string[]) => void;
}) {
    const [draft, setDraft] = useState<string[]>(worker.tags ?? []);
    const [input, setInput] = useState("");
    const [saving, setSaving] = useState(false);
    const [err, setErr] = useState<string | null>(null);

    useEffect(() => {
        setDraft(worker.tags ?? []);
    }, [worker.tags]);

    const allTagsQ = useQuery({ queryKey: ["admin", "worker-tags"], queryFn: listAllWorkerTags });
    const suggestions = useMemo(() => {
        const q = input.trim().toLowerCase();
        if (q.length === 0) return [];
        return (allTagsQ.data?.data ?? [])
            .filter((t) => t.startsWith(q) && !draft.includes(t))
            .slice(0, 6);
    }, [input, allTagsQ.data, draft]);

    function normalize(t: string): string | null {
        const v = t.trim().toLowerCase();
        if (!v) return null;
        // mirror the DB CHECK regex roughly
        if (!/^[a-z0-9][a-z0-9._:/-]*$/.test(v)) return null;
        if (v.length > 64) return null;
        return v;
    }

    function add(raw: string) {
        const t = normalize(raw);
        if (!t) return;
        if (draft.includes(t)) return;
        setDraft([...draft, t]);
        setInput("");
    }

    function remove(t: string) {
        setDraft(draft.filter((x) => x !== t));
    }

    async function save() {
        setSaving(true);
        setErr(null);
        try {
            const r = await setWorkerTags(worker.id, draft);
            setDraft(r.tags);
            onSaved?.(r.tags);
        } catch (e: any) {
            setErr(e?.message || "failed");
        } finally {
            setSaving(false);
        }
    }

    const dirty =
        draft.length !== (worker.tags ?? []).length ||
        draft.some((t, i) => t !== (worker.tags ?? [])[i]);

    return (
        <div>
            <div className="flex flex-wrap gap-1 mb-2">
                {draft.length === 0 && (
                    <span className="text-slate-400 text-xs italic">no tags yet</span>
                )}
                {draft.map((t) => (
                    <UserTag key={t} tag={t} onRemove={() => remove(t)} />
                ))}
            </div>
            <div className="relative">
                <input
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === ",") {
                            e.preventDefault();
                            add(input);
                        }
                        if (e.key === "Backspace" && input === "" && draft.length > 0) {
                            remove(draft[draft.length - 1]);
                        }
                    }}
                    placeholder="add a tag (eu-west, hetzner, warmup-only) and press enter"
                    className="w-full border rounded px-3 py-1.5 text-sm"
                />
                {suggestions.length > 0 && (
                    <div className="absolute top-full left-0 right-0 mt-1 border rounded bg-white shadow z-10 max-h-40 overflow-auto">
                        {suggestions.map((s) => (
                            <button
                                key={s}
                                onClick={() => add(s)}
                                className="block w-full text-left px-3 py-1.5 text-sm hover:bg-blue-50"
                            >
                                {s}
                            </button>
                        ))}
                    </div>
                )}
            </div>
            <div className="flex items-center gap-2 mt-2">
                <button
                    onClick={save}
                    disabled={!dirty || saving}
                    className="bg-blue-600 hover:bg-blue-700 text-white px-3 py-1.5 rounded text-xs disabled:opacity-50"
                >
                    {saving ? "Saving…" : "Save tags"}
                </button>
                {err && <span className="text-red-600 text-xs">{err}</span>}
            </div>
            <p className="text-slate-400 text-[11px] mt-2">
                Tags are free-form. The smart labels (tier:, pool:, state:, ver:, liveness:) are
                computed automatically — don't add them as tags.
            </p>
        </div>
    );
}
