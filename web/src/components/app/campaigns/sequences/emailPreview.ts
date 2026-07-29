// Shared email-template helpers: plain-text derivation, a faithful client-side
// model of the documented send-time render subset ({{variables}}, conditionals,
// spintax), and a malformed-template heuristic. Used by the step composer and
// the A/B variant editor so both preview and validate content identically.

// The standard merge fields and their sample values live in one catalog
// (@/lib/templateVars); imported for local use (renderPreview's default context)
// and re-exported so existing imports keep working.
import { VARIABLES, SAMPLE } from "@/lib/templateVars";
export { VARIABLES, SAMPLE };

// Derive plain text from the editor HTML so both alternatives ship populated.
export function htmlToPlain(html: string): string {
    const withBreaks = html
        .replace(/<\s*br\s*\/?>/gi, "\n")
        .replace(/<\/\s*(p|div|h[1-6]|li|tr)\s*>/gi, "\n");
    if (typeof document === "undefined") return withBreaks.replace(/<[^>]+>/g, "");
    const tmp = document.createElement("div");
    tmp.innerHTML = withBreaks;
    return (tmp.textContent || "").replace(/\n{3,}/g, "\n\n").trim();
}

// promptToHtml wraps a plain template string (a stored AI-block instruction) into
// editor HTML: each line becomes a paragraph and the text is escaped. The editor
// then upgrades any {{.Field}} tokens into chips itself, so this only needs to
// produce safe paragraph HTML. htmlToPlain is the inverse on the way back out.
export function promptToHtml(text: string): string {
    if (!text.trim()) return "";
    const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    return text
        .split(/\r?\n/)
        .map((line) => `<p>${line ? esc(line) : "<br>"}</p>`)
        .join("");
}

type PreviewCtx = Record<string, string>;
const truthy = (v: string | undefined): boolean => v !== undefined && v !== "";

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

function evalCond(expr: string, ctx: PreviewCtx): boolean {
    expr = expr.trim();
    // `not <expr>` — negate recursively (so `not (eq .X "v")` composes). The
    // builder emits this for the "is empty" operator; without it the client
    // preview always took the wrong branch.
    const notM = expr.match(/^not\s+([\s\S]+)$/);
    if (notM) return !evalCond(notM[1].replace(/^\(|\)$/g, ""), ctx);
    // Fields may contain spaces/dashes (custom fields), so widen the class.
    let m = expr.match(/^eq\s+\.([A-Za-z0-9_ -]+?)\s+"([^"]*)"$/);
    if (m) return (ctx[m[1]] ?? "") === m[2];
    m = expr.match(/^ne\s+\.([A-Za-z0-9_ -]+?)\s+"([^"]*)"$/);
    if (m) return (ctx[m[1]] ?? "") !== m[2];
    const logical = expr.match(/^(and|or)\s+(.*)$/s);
    if (logical) {
        const vals = splitGroups(logical[2]).map((p) => evalCond(p.replace(/^\(|\)$/g, ""), ctx));
        return logical[1] === "and" ? vals.every(Boolean) : vals.some(Boolean);
    }
    m = expr.match(/^\.([A-Za-z0-9_ -]+)$/);
    if (m) return truthy(ctx[m[1]]);
    return false;
}

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
    if (endIdx < 0) return s;
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

export function renderPreview(s: string, ctx: PreviewCtx = SAMPLE): string {
    let out = renderConditionals(s, ctx);
    out = out.replace(/\{\{\s*\.([A-Za-z0-9_]+)\s*\}\}/g, (_, k: string) => ctx[k] ?? "");
    out = out.replace(/\{([^{}|]+(?:\|[^{}]+)+)\}/g, (_, g: string) => g.split("|")[0]);
    return out;
}

// templateIssue returns a friendly message when a template is obviously
// malformed (an {{if}} without a matching {{end}}, or vice versa).
export function templateIssue(s: string): string | null {
    const ifs = (s.match(/\{\{\s*if\b/g) || []).length;
    const ends = (s.match(/\{\{\s*end\s*\}\}/g) || []).length;
    if (ifs > ends) return "An {{if}} is missing its {{end}}.";
    if (ends > ifs) return "There's an {{end}} with no matching {{if}}.";
    return null;
}
