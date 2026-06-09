// In-app quick reference for the templating + expression mini-language shared by
// campaign emails and automation conditions/actions. Click-to-copy snippets,
// grouped, with a link to the full guide. Mounted next to the Advanced
// expression editor (and reusable anywhere a "?" reference helps).

import toast from "react-hot-toast";
import { CircleHelpIcon, CopyIcon, ExternalLinkIcon } from "lucide-react";
import { PopoverMenu, PopoverMenuTrigger, PopoverMenuContent } from "@/components/ui/popover-menu";
import { WEBSITE_URL } from "@/lib/information";

interface Entry {
    code: string;
    label: string;
    note?: string;
}
interface Section {
    title: string;
    blurb: string;
    entries: Entry[];
}

const INTRO =
    "One Go text/template engine powers campaign emails and automation conditions/actions. Email merge fields use dotted PascalCase ({{.FirstName}}); automation event variables use the dotted event key ({{.contact_email}}). Always use the leading dot — it's standard Go template field access. Use the coercing gtf/ltf comparisons when a value might be a string. Bad templates never hard-fail; they fall back to the literal text.";

const SECTIONS: Section[] = [
    {
        title: "Variables",
        blurb: "Email: dotted PascalCase. Automations: the event key with a leading dot ({{.key}}).",
        entries: [
            { code: "{{.FirstName}}", label: "Contact field (email)", note: "Standard: .FirstName .LastName .Email .Company .Phone. Lowercase {{.firstname}} does NOT work for email merge fields." },
            { code: "{{.role}}", label: "Custom field by key", note: "Any key works, including names with spaces or dashes (e.g. {{.job title}}), in {{if}} and helpers too." },
            { code: "{{.contact_email}}", label: "Automation event variable", note: "Standard dotted field access — the leading dot is required. Unknown keys render empty." },
            { code: "{{.confidence}}", label: "Reply confidence (reply trigger)", note: "Stays numeric in both conditions and action values, so native gt/lt/eq work. Use gtf only when a value might arrive as text." },
        ],
    },
    {
        title: "Conditionals",
        blurb: "Standard control flow. A missing field is empty and tests false. Balance every {{if}} with {{end}}.",
        entries: [
            { code: "{{if .Company}}…{{end}}", label: "Only when the field is set" },
            { code: "{{if .Company}}…{{else}}…{{end}}", label: "If / else" },
            { code: '{{if eq .role "CEO"}}…{{end}}', label: "Equality test", note: "eq/ne/lt/gt compare same-kind values and do not coerce." },
            { code: "{{if and .FirstName .Company}}…{{end}}", label: "Both present (and / or / not)" },
            { code: 'gt .confidence 0.8', label: "Bare expression (automation condition)", note: "No braces needed; it auto-wraps. Blank = false." },
        ],
    },
    {
        title: "Fallback",
        blurb: "Fill a default when a field is blank. Signature is default(default, value).",
        entries: [
            { code: '{{.FirstName | default "there"}}', label: 'Use "there" when blank', note: "Pipeline form puts the value last, matching default(def, v)." },
            { code: '{{default "there" .FirstName}}', label: "Positional form (default first)", note: "Do not write default .FirstName \"there\"; the order is reversed." },
        ],
    },
    {
        title: "Numbers",
        blurb: "Math + compares coerce strings to numbers. Use the f-variants when a value might be a string.",
        entries: [
            { code: "{{gtf .confidence 0.8}}", label: "Coercing > (gtf ltf gef lef)", note: "Coerces both sides to a number first; non-numeric becomes 0." },
            { code: "{{num .confidence}}", label: "Force a value to a number" },
            { code: "{{add .a .b}}", label: "Add (also sub, mul)" },
            { code: "{{div .total .count}}", label: "Divide (mod too)", note: "Divisor 0 returns 0, never an error." },
        ],
    },
    {
        title: "Text",
        blurb: "String helpers. Watch the case-sensitivity notes.",
        entries: [
            { code: "{{title .FirstName}}", label: "Title-case (also upper, lower)" },
            { code: "{{trim .role}}", label: "Trim outer whitespace" },
            { code: '{{contains .Email "gmail"}}', label: "Substring test (case-insensitive)" },
            { code: '{{hasPrefix .Email "info@"}}', label: "Prefix test (case-sensitive)" },
        ],
    },
    {
        title: "Spintax",
        blurb: "Random alternation, expanded per recipient after the merge fields render.",
        entries: [
            { code: "{Hi|Hey|Hello}", label: "Pick one at random", note: "Only groups with a | expand; plain {…} is left untouched, so CSS is safe." },
            { code: "{Hi|Hey} {{.FirstName}}", label: "Combine with merge fields" },
        ],
    },
];

const EXAMPLES: { title: string; code: string; explain: string }[] = [
    {
        title: "Greeting with safe fallback",
        code: '{Hi|Hey} {{.FirstName | default "there"}},',
        explain: 'Falls back to "there" when the name is blank, then picks Hi or Hey at random per recipient.',
    },
    {
        title: "Conditional line on a custom field",
        code: "{{if .role}}Saw you lead {{title .role}} at {{.Company}}.{{end}}",
        explain: "Only emits the sentence when the custom field is present, and title-cases it.",
    },
    {
        title: "Automation: high-confidence reply",
        code: "gt .confidence 0.8",
        explain: "A bare condition expression; native gt works because condition data stays numeric.",
    },
    {
        title: "Action value: full template, not just substitution",
        code: "{{if gt .confidence 0.8}}hot lead{{else}}follow up{{end}}",
        explain: "Slack/webhook/CRM values render against the native event data — conditionals, ranges, nested fields, and native numeric compares all work. Reach for gtf only when a value might arrive as text.",
    },
];

function Code({ code }: { code: string }) {
    return (
        <button
            type="button"
            title="Click to copy"
            onClick={() => {
                navigator.clipboard?.writeText(code);
                toast.success("Copied");
            }}
            className="group inline-flex max-w-full items-center gap-1 rounded border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-left font-mono text-[11px] text-slate-700 transition-colors hover:border-sky-300 hover:bg-sky-50/40"
        >
            <span className="truncate">{code}</span>
            <CopyIcon className="w-2.5 h-2.5 shrink-0 text-slate-300 group-hover:text-sky-500" />
        </button>
    );
}

export function ExpressionReference({ label = "Reference" }: { label?: string }) {
    return (
        <PopoverMenu align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    className="inline-flex h-6 items-center gap-1 rounded px-1.5 text-[11px] text-slate-400 transition-colors hover:bg-sky-50 hover:text-sky-600"
                >
                    <CircleHelpIcon className="w-3.5 h-3.5" /> {label}
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent className="w-[380px] max-w-[92vw] max-h-[70vh] overflow-y-auto p-3">
                <div className="text-[12px] font-medium text-slate-900">Variables, conditions & functions</div>
                <p className="mt-1 text-[11px] leading-relaxed text-slate-500">{INTRO}</p>

                {SECTIONS.map((s) => (
                    <div key={s.title} className="mt-3 border-t border-slate-100 pt-2.5">
                        <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">{s.title}</div>
                        <p className="mt-0.5 text-[11px] leading-relaxed text-slate-500">{s.blurb}</p>
                        <div className="mt-1.5 space-y-1.5">
                            {s.entries.map((e, i) => (
                                <div key={i}>
                                    <Code code={e.code} />
                                    <div className="mt-0.5 text-[11px] text-slate-600">{e.label}</div>
                                    {e.note && <div className="text-[10.5px] leading-relaxed text-slate-400">{e.note}</div>}
                                </div>
                            ))}
                        </div>
                    </div>
                ))}

                <div className="mt-3 border-t border-slate-100 pt-2.5">
                    <div className="text-[10px] font-medium uppercase tracking-[0.14em] text-slate-400">Examples</div>
                    <div className="mt-1.5 space-y-2.5">
                        {EXAMPLES.map((ex, i) => (
                            <div key={i}>
                                <div className="mb-0.5 text-[11px] font-medium text-slate-700">{ex.title}</div>
                                <Code code={ex.code} />
                                <div className="mt-0.5 text-[10.5px] leading-relaxed text-slate-400">{ex.explain}</div>
                            </div>
                        ))}
                    </div>
                </div>

                <a
                    href={`${WEBSITE_URL}/learn/personalization`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="mt-3 inline-flex items-center gap-1 border-t border-slate-100 pt-2.5 text-[11px] font-medium text-sky-600 hover:text-sky-700"
                >
                    Full guide &amp; examples <ExternalLinkIcon className="w-3 h-3" />
                </a>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
