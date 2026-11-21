// Pure reading and rewriting of a campaign's step graph for the tree builder.
// A step's branches become ports: each conditional branch is its own column
// (checked left to right, first match wins), the catch-all is "Otherwise", a
// switch has one column per case. A branch routed nowhere (null target) ends
// the sequence and draws as an End terminal.

import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import type { BranchCondition, SequenceBranch } from "@/lib/api/models/app/campaigns/sequences/Branching";
import { BRANCH_FIELD_LABELS, isReplyBranchField, isInstantCapableField, replyIntentLabel } from "@/lib/api/models/app/campaigns/sequences/Branching";
import type { BranchTone, FlowPort, FlowSource } from "@/components/app/flow/tree";
import { reachableFrom } from "@/components/app/flow/tree";

export const ENTRY_ID = "__entry__";
const STOP_PREFIX = "stop:";
export const ELSE_PORT = "else";
const BRANCH_PREFIX = "b:";
const CASE_PREFIX = "case:";

export const isStopId = (id: string) => id.startsWith(STOP_PREFIX);
const stopId = (from: string, port: string) => `${STOP_PREFIX}${from}:${port}`;

export const isCond = (b: SequenceBranch) => (b.conditions?.length ?? 0) > 0;
export const caseKey = (name: string) => name.trim().toLowerCase();
export const isSwitchStep = (s?: Sequence) => s?.kind === "action" && s.action?.type === "switch";
export const isRouterStep = (s?: Sequence) => s?.kind === "wait";

export function newBranchId(): string {
    try {
        return crypto.randomUUID();
    } catch {
        return `b_${Math.floor(performance.now())}_${Math.random().toString(36).slice(2, 8)}`;
    }
}

// The order the backend checks them in: conditions first, then the catch-all.
export function ordered(branches: SequenceBranch[]): SequenceBranch[] {
    return [...branches.filter(isCond), ...branches.filter((b) => !isCond(b))];
}

// Only the first catch-all is ever followed; extras are dropped on every write.
export function normalize(branches: SequenceBranch[]): SequenceBranch[] {
    const conds = branches.filter(isCond);
    const fallback = branches.find((b) => !isCond(b));
    return fallback ? [...conds, fallback] : conds;
}

export const catchAll = (s?: Sequence) => (s?.conditions?.branches ?? []).find((b) => !isCond(b));

const caseOf = (b: SequenceBranch) => (b.conditions ?? []).find((c) => c.field === "ai_label")?.label?.trim() ?? "";

export function isInstantBranch(b: SequenceBranch): boolean {
    return (b.conditions ?? []).some((c) => isInstantCapableField(c.field)) && b.instant !== false;
}

const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s);

export function conditionText(b: SequenceBranch): string {
    return cap(
        (b.conditions ?? [])
            .map((c: BranchCondition) => {
                if (c.field === "random") return `${c.value ?? 50}% random`;
                if (c.field === "ai_label") return c.label ?? "case";
                if (c.field === "reply_intent") return `intent: ${replyIntentLabel(c.label)}`;
                const f = BRANCH_FIELD_LABELS[c.field] ?? c.field;
                if (isReplyBranchField(c.field)) return f;
                if (c.operator === "ever") return f;
                return `${f} within ${c.value ?? 3}d`;
            })
            .join(" + "),
    );
}

function branchTone(b: SequenceBranch): BranchTone {
    const f = b.conditions?.[0]?.field;
    if (!f) return "no";
    if (f.startsWith("not_")) return "no";
    if (f === "random" || f === "ai_label" || f === "reply_intent") return "case";
    return "yes";
}

export function portOfBranch(s: Sequence, b: SequenceBranch): string {
    if (!isCond(b)) return ELSE_PORT;
    if (isSwitchStep(s)) {
        const name = caseOf(b);
        const cases = (s.action?.switch_cases ?? []).map(caseKey);
        if (name && cases.includes(caseKey(name))) return CASE_PREFIX + caseKey(name);
    }
    return BRANCH_PREFIX + b.branch_id;
}

// The branch a port stands for, if one exists yet.
export function branchAt(s: Sequence | undefined, port: string): SequenceBranch | undefined {
    if (!s) return undefined;
    return (s.conditions?.branches ?? []).find((b) => portOfBranch(s, b) === port);
}

export const casePort = (name: string) => CASE_PREFIX + caseKey(name);

export function stepPorts(s: Sequence): FlowPort[] {
    const branches = s.conditions?.branches ?? [];
    const to = (b: SequenceBranch | undefined, port: string) => (b ? [b.target_step_id ?? stopId(s.id, port)] : []);
    const ports: FlowPort[] = [];
    const fallback = catchAll(s);
    if (isSwitchStep(s)) {
        const cases = (s.action?.switch_cases ?? []).map((c) => c.trim()).filter(Boolean);
        for (const c of cases) {
            const port = casePort(c);
            ports.push({ port, label: c, tone: "case", showEmpty: true, targets: to(branchAt(s, port), port) });
        }
        for (const b of branches.filter(isCond)) {
            const port = portOfBranch(s, b);
            if (port.startsWith(CASE_PREFIX)) continue;
            ports.push({ port, label: `${conditionText(b)} (removed)`, tone: "warn", targets: to(b, port) });
        }
        ports.push({ port: ELSE_PORT, label: "Otherwise", tone: "no", showEmpty: cases.length > 0, targets: to(fallback, ELSE_PORT) });
        return ports;
    }
    const conds = branches.filter(isCond);
    for (const b of conds) {
        const port = portOfBranch(s, b);
        ports.push({ port, label: conditionText(b) + (isInstantBranch(b) ? " · instant" : ""), tone: branchTone(b), targets: to(b, port) });
    }
    if (conds.length) ports.push({ port: ELSE_PORT, label: "Otherwise", tone: "no", showEmpty: true, targets: to(fallback, ELSE_PORT) });
    else ports.push({ port: ELSE_PORT, targets: to(fallback, ELSE_PORT) });
    return ports;
}

export function campaignSource(steps: Sequence[], size: FlowSource["size"], note?: FlowSource["note"]): FlowSource {
    const byId = new Map(steps.map((s) => [s.id, s]));
    const first = steps[0];
    const portsOf = (id: string): FlowPort[] => {
        if (id === ENTRY_ID) return [{ port: "", targets: first ? [first.id] : [], noInsert: !!first }];
        const s = byId.get(id);
        return s ? stepPorts(s) : [];
    };
    const stops: string[] = [];
    for (const s of steps) for (const p of stepPorts(s)) for (const t of p.targets) if (isStopId(t)) stops.push(t);
    return {
        roots: [ENTRY_ID],
        nodeIds: [ENTRY_ID, ...steps.map((s) => s.id), ...stops],
        ports: portsOf,
        size,
        appendPort: (id) => (isStopId(id) ? null : id === ENTRY_ID ? "" : ELSE_PORT),
        note,
    };
}

// Steps the first step leads to; anything else never runs.
export function reachableSteps(steps: Sequence[]): Set<string> {
    if (!steps[0]) return new Set();
    const byId = new Map(steps.map((s) => [s.id, s]));
    return reachableFrom({ ports: (id) => (byId.get(id) ? stepPorts(byId.get(id)!) : []) }, steps[0].id);
}

// Steps `from` may continue at without looping back to itself.
export function gotoCandidates(steps: Sequence[], from: string): Sequence[] {
    const byId = new Map(steps.map((s) => [s.id, s]));
    const ports = (id: string) => (byId.get(id) ? stepPorts(byId.get(id)!) : []);
    return steps.filter((s) => s.id !== from && !reachableFrom({ ports }, s.id).has(from));
}

// Point one port of a step at `target` (null ends the sequence there), creating
// the branch the port stands for when it has none yet.
export function routePort(s: Sequence, port: string, target: string | null): SequenceBranch[] {
    const branches = s.conditions?.branches ?? [];
    const existing = branchAt(s, port);
    if (existing) return normalize(branches.map((b) => (b === existing ? { ...b, target_step_id: target } : b)));
    if (port === ELSE_PORT) return normalize([...branches, { branch_id: newBranchId(), target_step_id: target, conditions: [] }]);
    if (port.startsWith(CASE_PREFIX)) {
        const name = (s.action?.switch_cases ?? []).find((c) => casePort(c) === port)?.trim();
        if (!name) return branches;
        return normalize([...branches, { branch_id: newBranchId(), target_step_id: target, conditions: [{ field: "ai_label", operator: "is", label: name }] }]);
    }
    return branches;
}

export const DEFAULT_CONDITION: BranchCondition = { field: "opened", operator: "within_days", value: 3 };
export const REPLY_CONDITION: BranchCondition = { field: "reply_positive", operator: "ever" };

// A new conditional path, not on any step yet (it ends the sequence until steps go under it).
export const draftCondition = (cond: BranchCondition): SequenceBranch => ({ branch_id: newBranchId(), target_step_id: null, conditions: [cond] });

// A conditional path added to a step, checked after its existing ones.
export function insertCondition(s: Sequence, branch: SequenceBranch): SequenceBranch[] {
    const all = s.conditions?.branches ?? [];
    return normalize([...all.filter(isCond), branch, ...all.filter((b) => !isCond(b))]);
}

export function addCondition(s: Sequence, cond: BranchCondition): { branches: SequenceBranch[]; branch: SequenceBranch } {
    const branch = draftCondition(cond);
    return { branches: insertCondition(s, branch), branch };
}

export function moveCondition(s: Sequence, branchId: string, dir: -1 | 1): SequenceBranch[] {
    const all = s.conditions?.branches ?? [];
    const conds = all.filter(isCond);
    const i = conds.findIndex((b) => b.branch_id === branchId);
    const j = i + dir;
    if (i < 0 || j < 0 || j >= conds.length) return all;
    [conds[i], conds[j]] = [conds[j], conds[i]];
    return normalize([...conds, ...all.filter((b) => !isCond(b))]);
}

export interface StepRemoval {
    // Steps whose branches change, with their new branch lists.
    rewrites: { id: string; branches: SequenceBranch[] }[];
    // The one step the removed one led to, which now takes its place.
    successor: string | null;
    // Steps that lose their only way in and will never run.
    stranded: string[];
}

// Removing a step reconnects what led to it to what it led to when that is a
// single step. Otherwise a conditional path into it ends there (the column
// stays) and a plain one is dropped, and the steps after it are left
// unconnected rather than deleted.
export function planRemoval(steps: Sequence[], id: string): StepRemoval {
    const gone = steps.find((s) => s.id === id);
    const branches = gone?.conditions?.branches ?? [];
    // Bridge only through the catch-all: a conditional path never becomes the way on for everyone.
    const conditional = branches.some((b) => isCond(b) && b.target_step_id && b.target_step_id !== id);
    const outs = [...new Set(branches.filter((b) => !isCond(b)).map((b) => b.target_step_id).filter((t): t is string => !!t && t !== id))];
    const successor = !conditional && outs.length === 1 ? outs[0] : null;
    const rewrites: StepRemoval["rewrites"] = [];
    for (const s of steps) {
        if (s.id === id) continue;
        const branches = s.conditions?.branches ?? [];
        if (!branches.some((b) => b.target_step_id === id)) continue;
        const next = branches.flatMap((b) => {
            if (b.target_step_id !== id) return [b];
            if (successor) return [{ ...b, target_step_id: successor }];
            return isCond(b) ? [{ ...b, target_step_id: null }] : [];
        });
        rewrites.push({ id: s.id, branches: normalize(next) });
    }
    const after = steps
        .filter((s) => s.id !== id)
        .map((s) => {
            const r = rewrites.find((x) => x.id === s.id);
            return r ? { ...s, conditions: { branches: r.branches } } : s;
        });
    const before = reachableSteps(steps);
    const now = reachableSteps(after);
    const stranded = after.filter((s) => before.has(s.id) && !now.has(s.id)).map((s) => s.id);
    return { rewrites, successor, stranded };
}

// Rough content check for an email body: markup with no text and no image is empty.
export function emailBodyEmpty(html: string): boolean {
    if (/<img\b/i.test(html)) return false;
    return html.replace(/<[^>]*>/g, "").replace(/&nbsp;/g, " ").trim() === "";
}
