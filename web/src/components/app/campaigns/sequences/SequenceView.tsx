// The campaign Step composer (right pane). A full email editor: rich TipTap
// body, subject with variable insert, a template picker + save-as-template
// (reusing the org template library), an Edit/Preview toggle that renders
// {{variables}} + spintax with sample data, and the advisory content score.

import React from "react";
import {
    AlertCircleIcon,
    BookmarkPlusIcon,
    EyeIcon,
    GitBranchIcon,
    Loader2Icon,
    PencilLineIcon,
    SparklesIcon,
} from "lucide-react";
import toast from "react-hot-toast";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import RichTextEditor, { VariableMenu } from "./RichTextEditor";
import { useTemplatePreview } from "@/lib/api/hooks/app/campaigns/useTemplatePreview";
import type { TemplatePreview } from "@/lib/api/client/app/campaigns/previewTemplate";
import WriteWithAI from "./WriteWithAI";
import ContentScore from "../ContentScore";
import { Label, TextInput } from "@/components/ui/field";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
    SelectButton,
} from "@/components/ui/popover-menu";
import useUpdateSequence from "@/lib/api/hooks/app/campaigns/sequences/useUpdateSequence";
import useTemplates from "@/lib/api/hooks/app/templates/useTemplates";
import useCreateTemplate from "@/lib/api/hooks/app/templates/useCreateTemplate";
import { useConfirm } from "@/hooks/context/confirm";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const VARIABLES = ["{{.FirstName}}", "{{.LastName}}", "{{.Email}}", "{{.Company}}", "{{.Phone}}"];
const SAMPLE: Record<string, string> = {
    FirstName: "Alex",
    LastName: "Rivera",
    Email: "alex@acme.com",
    Company: "Acme",
    Phone: "+1 555-0100",
    // Sample custom fields so conditional examples have something to evaluate.
    role: "Engineer",
    city: "Berlin",
};

// Body fields the composer owns. body_sync/body_code are legacy editor-only
// flags (they don't affect sending), so the new composer keeps HTML + plain in
// lockstep and leaves them alone.
type Draft = Pick<Sequence, "name" | "subject" | "body_plain" | "body_html">;

function toDraft(s: Sequence): Draft {
    return { name: s.name, subject: s.subject, body_plain: s.body_plain, body_html: s.body_html };
}

// Derive plain text from the editor HTML so both alternatives ship populated.
function htmlToPlain(html: string): string {
    const withBreaks = html
        .replace(/<\s*br\s*\/?>/gi, "\n")
        .replace(/<\/\s*(p|div|h[1-6]|li|tr)\s*>/gi, "\n");
    if (typeof document === "undefined") return withBreaks.replace(/<[^>]+>/g, "");
    const tmp = document.createElement("div");
    tmp.innerHTML = withBreaks;
    return (tmp.textContent || "").replace(/\n{3,}/g, "\n\n").trim();
}

// Preview evaluator — a faithful JS model of the documented send-time subset:
// {{if}}/{{else}}/{{else if}}/{{end}}, {{if eq .x "y"}}, {{if and|or ...}},
// variables/custom fields, and spintax (first option). Mirrors the Go renderer:
// missing or empty values are falsy in conditions, missing keys render empty.
type PreviewCtx = Record<string, string>;

const truthy = (v: string | undefined): boolean => v !== undefined && v !== "";

// Split an and/or argument list into top-level groups: '(.A) (eq .B "x")'.
function splitGroups(s: string): string[] {
    const out: string[] = [];
    let depth = 0;
    let cur = "";
    for (const ch of s) {
        if (ch === "(") {
            if (depth === 0 && cur.trim()) {
                out.push(cur.trim());
                cur = "";
            }
            depth++;
            cur += ch;
        } else if (ch === ")") {
            depth--;
            cur += ch;
            if (depth === 0) {
                out.push(cur.trim());
                cur = "";
            }
        } else if (ch === " " && depth === 0) {
            if (cur.trim()) out.push(cur.trim());
            cur = "";
        } else {
            cur += ch;
        }
    }
    if (cur.trim()) out.push(cur.trim());
    return out.filter(Boolean);
}

// Evaluate a single {{if ...}} condition (the part after "if ").
function evalCond(expr: string, ctx: PreviewCtx): boolean {
    expr = expr.trim();
    let m = expr.match(/^eq\s+\.([A-Za-z0-9_]+)\s+"([^"]*)"$/);
    if (m) return (ctx[m[1]] ?? "") === m[2];
    const logical = expr.match(/^(and|or)\s+(.*)$/s);
    if (logical) {
        const vals = splitGroups(logical[2]).map((p) => evalCond(p.replace(/^\(|\)$/g, ""), ctx));
        return logical[1] === "and" ? vals.every(Boolean) : vals.some(Boolean);
    }
    m = expr.match(/^\.([A-Za-z0-9_]+)$/);
    if (m) return truthy(ctx[m[1]]);
    return false; // unknown construct → false (degrade, don't crash preview)
}

// Resolve {{if}}…{{end}} blocks recursively (handles nesting + else/else-if).
function renderConditionals(s: string, ctx: PreviewCtx): string {
    const open = s.match(/\{\{\s*if\s+([^}]+?)\s*\}\}/);
    if (!open || open.index === undefined) return s;
    const start = open.index;
    const tokenRe = /\{\{\s*(if\s+[^}]+?|else\s+if\s+[^}]+?|else|end)\s*\}\}/g;
    tokenRe.lastIndex = start;
    let depth = 0;
    let endIdx = -1;
    let endLen = 0;
    const branches: { cond: string | null; from: number; bodyStart: number }[] = [];
    let m: RegExpExecArray | null;
    while ((m = tokenRe.exec(s))) {
        const kind = m[1];
        if (kind.startsWith("if")) {
            depth++;
            if (depth === 1) branches.push({ cond: kind.slice(2).trim(), from: m.index, bodyStart: tokenRe.lastIndex });
        } else if (depth === 1 && kind.startsWith("else if")) {
            branches[branches.length - 1].from = m.index;
            branches.push({ cond: kind.slice(7).trim(), from: m.index, bodyStart: tokenRe.lastIndex });
        } else if (depth === 1 && kind === "else") {
            branches[branches.length - 1].from = m.index;
            branches.push({ cond: null, from: m.index, bodyStart: tokenRe.lastIndex });
        } else if (kind === "end") {
            depth--;
            if (depth === 0) {
                endIdx = m.index;
                endLen = m[0].length;
                break;
            }
        }
    }
    if (endIdx < 0) return s; // unbalanced → leave as-is (matches send fallback)
    let chosen = "";
    for (let i = 0; i < branches.length; i++) {
        const b = branches[i];
        const bodyEnd = i + 1 < branches.length ? branches[i + 1].from : endIdx;
        if (b.cond === null || evalCond(b.cond, ctx)) {
            chosen = renderConditionals(s.slice(b.bodyStart, bodyEnd), ctx);
            break;
        }
    }
    return renderConditionals(s.slice(0, start), ctx) + chosen + renderConditionals(s.slice(endIdx + endLen), ctx);
}

function renderPreview(s: string, ctx: PreviewCtx = SAMPLE): string {
    let out = renderConditionals(s, ctx);
    out = out.replace(/\{\{\s*\.([A-Za-z0-9_]+)\s*\}\}/g, (_, k: string) => ctx[k] ?? "");
    out = out.replace(/\{([^{}|]+(?:\|[^{}]+)+)\}/g, (_, g: string) => g.split("|")[0]);
    return out;
}

// templateIssue returns a friendly message when a template is obviously
// malformed (an {{if}} without a matching {{end}}, or vice versa). A heuristic
// for instant editor feedback — the renderer still degrades safely at send time.
function templateIssue(s: string): string | null {
    const ifs = (s.match(/\{\{\s*if\b/g) || []).length;
    const ends = (s.match(/\{\{\s*end\s*\}\}/g) || []).length;
    if (ifs > ends) return "An {{if}} is missing its {{end}}.";
    if (ends > ifs) return "There's an {{end}} with no matching {{if}}.";
    return null;
}

export default function SequenceView({
    campaignId,
    sequence,
    index,
}: {
    campaignId: string;
    sequence: Sequence;
    index: number;
}) {
    const updateSequence = useUpdateSequence(campaignId, sequence.id);
    const createTemplate = useCreateTemplate();
    const confirm = useConfirm();
    const { data: templates } = useTemplates("");

    const [load, setLoad] = React.useState(false);
    const [tab, setTab] = React.useState<"edit" | "preview">("edit");
    const [saveTplOpen, setSaveTplOpen] = React.useState(false);
    const [tplName, setTplName] = React.useState("");

    const [draft, setDraft] = React.useState<Draft>(() => toDraft(sequence));
    React.useEffect(() => {
        setDraft(toDraft(sequence));
        setTab("edit");
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [sequence.id]);

    const baseline = toDraft(sequence);
    const savable = React.useMemo(
        () => JSON.stringify(baseline) !== JSON.stringify(draft),
        [baseline, draft],
    );
    const patch = (p: Partial<Draft>) => setDraft((d) => ({ ...d, ...p }));
    const setBody = (htmlText: string) => patch({ body_html: htmlText, body_plain: htmlToPlain(htmlText) });

    // Instant feedback for an obviously-broken conditional (unbalanced if/end).
    // Covers the plain-text body too — it's rendered at send time and the
    // backend blocks campaign start on a malformed template in any of the three.
    const tplIssue =
        templateIssue(draft.subject) || templateIssue(draft.body_html) || templateIssue(draft.body_plain);

    // Authoritative preview: render through the REAL send-time engine (functions,
    // conditionals, spintax all run) against a sample contact, debounced. The
    // local renderPreview is the instant fallback shown while the request is in
    // flight; the server result is the source of truth for errors + unresolved
    // tokens.
    const previewMut = useTemplatePreview();
    const runPreview = previewMut.mutateAsync;
    const [serverPreview, setServerPreview] = React.useState<TemplatePreview | null>(null);
    React.useEffect(() => {
        if (tab !== "preview") return;
        const t = setTimeout(() => {
            runPreview({ subject: draft.subject, body_html: draft.body_html, body_plain: draft.body_plain })
                .then(setServerPreview)
                .catch(() => setServerPreview(null));
        }, 250);
        return () => clearTimeout(t);
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [tab, draft.subject, draft.body_html, draft.body_plain]);

    async function submit() {
        if (load || !savable) return;
        setLoad(true);
        try {
            const data: Partial<Sequence> = {
                ...(draft.name !== baseline.name && { name: draft.name }),
                ...(draft.subject !== baseline.subject && { subject: draft.subject }),
                ...(draft.body_plain !== baseline.body_plain && { body_plain: draft.body_plain }),
                ...(draft.body_html !== baseline.body_html && { body_html: draft.body_html }),
            };
            await toast.promise(updateSequence.mutateAsync(data), {
                loading: "Saving step…",
                success: "Step saved.",
                error: (err: AppError) => buildError(err),
            });
        } finally {
            setLoad(false);
        }
    }

    function applyTemplate(t: { name: string; subject: string; body_html: string; body_plain: string }) {
        const apply = () => {
            patch({
                subject: t.subject || draft.subject,
                body_html: t.body_html || (t.body_plain ? `<p>${t.body_plain.replace(/\n/g, "</p><p>")}</p>` : ""),
                body_plain: t.body_plain || htmlToPlain(t.body_html),
            });
            toast.success(`Applied "${t.name}"`);
        };
        const dirty = draft.subject.trim() || htmlToPlain(draft.body_html).trim();
        if (dirty) {
            confirm.show(`Replace this step's content with the "${t.name}" template?`, apply);
        } else {
            apply();
        }
    }

    async function saveAsTemplate() {
        const name = tplName.trim();
        if (!name) return;
        await toast.promise(
            createTemplate.mutateAsync({
                name,
                subject: draft.subject,
                body_html: draft.body_html,
                body_plain: draft.body_plain,
            }),
            { loading: "Saving template…", success: "Saved to template library.", error: (e: AppError) => buildError(e) },
        );
        setSaveTplOpen(false);
        setTplName("");
    }

    return (
        <div className="rounded-md border border-slate-200 bg-white">
            <div className="flex flex-col gap-3 border-b border-slate-200 px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-400 font-medium">
                        Step {index + 1}
                    </div>
                    <p className="mt-0.5 truncate text-[11px] text-slate-400">Compose the email this step sends.</p>
                </div>
                <div className="flex flex-wrap shrink-0 items-center gap-2">
                    {/* Template picker */}
                    <PopoverMenu>
                        <PopoverMenuTrigger asChild>
                            <SelectButton icon={<SparklesIcon className="w-3.5 h-3.5" />} label="Templates" />
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={260} className="max-h-72 overflow-y-auto p-1">
                            {(templates ?? []).length === 0 ? (
                                <div className="px-2.5 py-3 text-center text-[11.5px] text-slate-400">
                                    No templates yet. Save one below.
                                </div>
                            ) : (
                                (templates ?? []).map((t) => (
                                    <button
                                        key={t.id}
                                        type="button"
                                        onClick={() => applyTemplate(t)}
                                        className="block w-full rounded px-2.5 py-1.5 text-left hover:bg-slate-100"
                                    >
                                        <div className="truncate text-[12.5px] font-medium text-slate-800">{t.name}</div>
                                        <div className="truncate text-[11px] text-slate-400">{t.subject || "No subject"}</div>
                                    </button>
                                ))
                            )}
                        </PopoverMenuContent>
                    </PopoverMenu>

                    {/* Save as template */}
                    <PopoverMenu open={saveTplOpen} onOpenChange={setSaveTplOpen}>
                        <PopoverMenuTrigger asChild>
                            <button
                                type="button"
                                title="Save this step as a reusable template"
                                className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900"
                            >
                                <BookmarkPlusIcon className="w-3.5 h-3.5" />
                                Save as template
                            </button>
                        </PopoverMenuTrigger>
                        <PopoverMenuContent minWidth={240} className="p-2">
                            <Label>Template name</Label>
                            <div className="flex items-center gap-1.5">
                                <TextInput
                                    value={tplName}
                                    onChange={setTplName}
                                    placeholder="e.g. Cold intro v1"
                                    className="flex-1"
                                />
                                <button
                                    type="button"
                                    onClick={saveAsTemplate}
                                    disabled={!tplName.trim() || createTemplate.isPending}
                                    className="h-7 px-2.5 rounded-md bg-sky-600 text-[12px] font-medium text-white hover:bg-sky-700 disabled:opacity-50"
                                >
                                    {createTemplate.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : "Save"}
                                </button>
                            </div>
                        </PopoverMenuContent>
                    </PopoverMenu>

                    <WriteWithAI
                        onInsert={(text) => {
                            const html = `<p>${text
                                .replace(/&/g, "&amp;")
                                .replace(/</g, "&lt;")
                                .replace(/>/g, "&gt;")
                                .replace(/\n{2,}/g, "</p><p>")
                                .replace(/\n/g, "<br />")}</p>`;
                            setBody(draft.body_html ? `${draft.body_html}${html}` : html);
                        }}
                    />
                    <div className="h-4 w-px bg-slate-200" />
                    <button
                        type="button"
                        onClick={() => setDraft(toDraft(sequence))}
                        disabled={!savable || load}
                        className="h-7 px-2.5 rounded-md border border-slate-200 bg-white text-[12px] font-medium text-slate-700 transition-colors hover:border-slate-300 hover:text-slate-900 disabled:opacity-40"
                    >
                        Reset
                    </button>
                    <button
                        type="button"
                        onClick={submit}
                        disabled={!savable || load}
                        className="h-7 px-3 rounded-md bg-sky-600 text-[12px] font-medium text-white transition-colors hover:bg-sky-700 inline-flex items-center gap-1.5 disabled:opacity-40"
                    >
                        {load && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Save changes
                    </button>
                </div>
            </div>

            <div className="space-y-4 p-3">
                <div>
                    <Label>Step name</Label>
                    <TextInput value={draft.name} onChange={(v) => patch({ name: v })} placeholder={`Step ${index + 1}`} />
                    <p className="mt-1.5 text-[10.5px] text-slate-400">Internal label only — recipients never see it.</p>
                </div>

                <div>
                    <div className="flex items-center justify-between gap-2 mb-1.5">
                        <Label className="mb-0">Subject</Label>
                        <VariableMenu variables={VARIABLES} onPick={(v) => patch({ subject: draft.subject + v })} />
                    </div>
                    <TextInput value={draft.subject} onChange={(v) => patch({ subject: v })} placeholder="Quick question, {{.FirstName}}" />
                </div>

                <div>
                    <div className="flex items-center justify-between gap-2 mb-1.5">
                        <Label className="mb-0">Body</Label>
                        <div className="inline-flex items-center gap-0.5 rounded-md bg-slate-100 p-0.5">
                            <TabBtn active={tab === "edit"} onClick={() => setTab("edit")} icon={<PencilLineIcon className="w-3 h-3" />}>
                                Edit
                            </TabBtn>
                            <TabBtn active={tab === "preview"} onClick={() => setTab("preview")} icon={<EyeIcon className="w-3 h-3" />}>
                                Preview
                            </TabBtn>
                        </div>
                    </div>
                    {tab === "edit" ? (
                        <RichTextEditor
                            key={sequence.id}
                            html={draft.body_html}
                            onChange={setBody}
                            variables={VARIABLES}
                            placeholder="Hi {{.FirstName}}, …"
                        />
                    ) : (
                        <div className="rounded-md border border-slate-200 bg-white">
                            <div className="border-b border-slate-200/70 px-3 py-2 text-[12.5px]">
                                <span className="text-slate-400">Subject: </span>
                                <span className="text-slate-800">{(serverPreview?.subject ?? renderPreview(draft.subject)) || "—"}</span>
                            </div>
                            <div
                                className="tiptap-body min-h-[200px] px-3 py-2.5 text-[13px] leading-relaxed text-slate-800"
                                dangerouslySetInnerHTML={{ __html: (serverPreview?.body_html ?? renderPreview(draft.body_html)) || '<p class="text-slate-300">Nothing to preview yet.</p>' }}
                            />
                            <p className="border-t border-slate-200/70 px-3 py-1.5 text-[10.5px] text-slate-400">
                                Rendered with the real send engine against sample data ({SAMPLE.FirstName} at {SAMPLE.Company}) — conditionals, functions, and spintax all run.
                            </p>
                        </div>
                    )}
                    {(serverPreview?.errors?.length ?? 0) > 0 ? (
                        <p className="mt-1.5 flex items-start gap-1.5 text-[11px] text-rose-600">
                            <AlertCircleIcon className="mt-px w-3.5 h-3.5 shrink-0" />
                            <span>{serverPreview!.errors!.join(" · ")} — fix before sending.</span>
                        </p>
                    ) : tplIssue ? (
                        <p className="mt-1.5 flex items-center gap-1.5 text-[11px] text-amber-600">
                            <AlertCircleIcon className="w-3.5 h-3.5 shrink-0" />
                            {tplIssue} It'll fall back to plain text — fix it before sending.
                        </p>
                    ) : null}
                    {(serverPreview?.unresolved?.length ?? 0) > 0 && (
                        <p className="mt-1.5 flex items-start gap-1.5 text-[11px] text-amber-600">
                            <AlertCircleIcon className="mt-px w-3.5 h-3.5 shrink-0" />
                            <span>
                                These won&apos;t resolve and would send literally:{" "}
                                <span className="font-mono">{serverPreview!.unresolved!.join(", ")}</span>
                            </span>
                        </p>
                    )}
                </div>

                {index > 0 && (
                    <div className="flex items-start gap-2 rounded-md border border-slate-200 bg-slate-50/60 px-3 py-2.5">
                        <GitBranchIcon className="mt-0.5 w-3.5 h-3.5 shrink-0 text-slate-400" />
                        <p className="text-[11px] leading-relaxed text-slate-500">
                            Follow-ups thread on the previous step&apos;s subject. Change this subject and the follow-up
                            starts a new thread instead of replying in the existing one.
                        </p>
                    </div>
                )}

                <ContentScore subject={draft.subject} bodyHtml={draft.body_html} bodyPlain={draft.body_plain} />
            </div>
        </div>
    );
}

function TabBtn({
    active,
    onClick,
    icon,
    children,
}: {
    active: boolean;
    onClick: () => void;
    icon: React.ReactNode;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className={`h-6 px-2 inline-flex items-center gap-1 rounded text-[11.5px] font-medium transition-colors ${
                active ? "bg-white text-slate-900 shadow-sm" : "text-slate-500 hover:text-slate-900"
            }`}
        >
            {icon}
            {children}
        </button>
    );
}
