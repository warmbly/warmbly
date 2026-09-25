// The workspace's own inbox tagging questions. A question is edited in a local
// form and only reaches the autosaved settings once it is complete, so a half
// typed question never makes the whole settings save fail validation.

import React from "react";
import { PencilIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";
import { NumberInput, TextInput } from "@/components/ui/field";
import { SelectMenu, type SelectOption } from "@/components/ui/select-menu";
import { useConfirm } from "@/hooks/context/confirm";
import { isAutomaticTag } from "@/lib/unibox/tagMeanings";
import {
    INBOX_TAG_CHOICES_MAX,
    INBOX_TAG_CHOICES_MIN,
    INBOX_TAG_CHOICE_DESC_MAX_LEN,
    INBOX_TAG_HOLD_DEFAULT,
    INBOX_TAG_HOLD_MAX,
    INBOX_TAG_HOLD_MIN,
    INBOX_TAG_LABEL_MAX_LEN,
    INBOX_TAG_QUESTIONS_MAX,
    INBOX_TAG_QUESTION_MAX_LEN,
    inboxTagLabelName,
    type InboxTagActionType,
    type InboxTagChoice,
    type InboxTagQuestion,
    type InboxTagQuestionAction,
} from "@/lib/api/models/app/outreach/OutreachSettings";

const ACTIONS: SelectOption[] = [
    { value: "", label: "Label only" },
    { value: "hold", label: "Hold the contact" },
    { value: "stop", label: "Stop the contact" },
    { value: "task", label: "Open a task" },
];

const SECONDARY =
    "h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed";
const PRIMARY =
    "h-7 px-3 rounded-md bg-slate-900 hover:bg-slate-800 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed";
const ICON_BUTTON =
    "h-7 w-7 rounded-md inline-flex items-center justify-center text-slate-400 hover:text-slate-700 hover:bg-slate-100 transition-colors";

function newID(): string {
    return crypto.randomUUID().replace(/-/g, "").slice(0, 12);
}

function blankChoice(): InboxTagChoice {
    return { label: "", description: "", action: { type: "" } };
}

function blankQuestion(): InboxTagQuestion {
    return { id: newID(), type: "yes_no", question: "", label: "", action: { type: "" } };
}

function labelsOf(q: InboxTagQuestion): string[] {
    return q.type === "choice"
        ? (q.choices ?? []).map((c) => c.label).filter(Boolean)
        : q.label
          ? [q.label]
          : [];
}

function actionSummary(a: InboxTagQuestionAction): string {
    switch (a.type) {
        case "hold":
            return `holds ${a.hold_days ?? INBOX_TAG_HOLD_DEFAULT} days`;
        case "stop":
            return "stops the contact";
        case "task":
            return "opens a task";
        default:
            return "";
    }
}

// problems mirrors the server's checks, so Save explains a refusal instead of
// the autosave failing after the fact.
function problems(q: InboxTagQuestion, taken: Set<string>): string[] {
    const out: string[] = [];
    if (!q.question.trim()) out.push("Write the question.");
    const seen = new Set<string>();
    const checkLabel = (raw: string, where: string) => {
        const name = inboxTagLabelName(raw);
        if (!name) {
            out.push(`${where} needs a label.`);
            return;
        }
        const key = name.toLowerCase();
        if ([...name].length > INBOX_TAG_LABEL_MAX_LEN) out.push(`"${name}" is longer than ${INBOX_TAG_LABEL_MAX_LEN} characters.`);
        if (isAutomaticTag(name)) out.push(`"${name}" is a built-in label. Pick another name.`);
        else if (taken.has(key) || seen.has(key)) out.push(`"${name}" is already used by another question or option.`);
        seen.add(key);
    };
    if (q.type === "yes_no") {
        checkLabel(q.label ?? "", "The question");
    } else {
        const choices = q.choices ?? [];
        if (choices.length < INBOX_TAG_CHOICES_MIN) out.push(`Add at least ${INBOX_TAG_CHOICES_MIN} options.`);
        choices.forEach((c, i) => {
            checkLabel(c.label, `Option ${i + 1}`);
            if (!c.description.trim()) out.push(`Option ${i + 1} needs a description.`);
        });
    }
    return out;
}

// finalize is what Save stores: labels as the slugs they file under, and only
// the fields the question type uses.
function finalize(q: InboxTagQuestion): InboxTagQuestion {
    const question = q.question.trim().replace(/\s+/g, " ");
    if (q.type === "yes_no") {
        return { id: q.id, type: "yes_no", question, label: inboxTagLabelName(q.label ?? ""), action: q.action };
    }
    return {
        id: q.id,
        type: "choice",
        question,
        action: { type: "" },
        choices: (q.choices ?? []).map((c) => ({
            label: inboxTagLabelName(c.label),
            description: c.description.trim().replace(/\s+/g, " "),
            action: c.action,
        })),
    };
}

export default function TaggingQuestions({
    value,
    onChange,
}: {
    value: InboxTagQuestion[];
    onChange: (next: InboxTagQuestion[]) => void;
}) {
    const confirm = useConfirm();
    // index null is a new question; the draft is the form's own copy.
    const [editing, setEditing] = React.useState<{ index: number | null; draft: InboxTagQuestion } | null>(null);
    const atMax = value.length >= INBOX_TAG_QUESTIONS_MAX;

    const open = (index: number | null) => {
        const draft = index === null ? blankQuestion() : structuredClone(value[index]);
        if (draft.type === "choice" && (draft.choices ?? []).length === 0) draft.choices = [blankChoice(), blankChoice()];
        setEditing({ index, draft });
    };

    const remove = (index: number) => {
        const q = value[index];
        confirm.show(
            `Remove the question "${q.question}"? Labels it already applied stay on their threads.`,
            async () => {
                onChange(value.filter((_, i) => i !== index));
                if (editing?.index === index) setEditing(null);
            },
        );
    };

    return (
        <div className="space-y-2">
            {value.length === 0 && !editing && (
                <p className="text-[11.5px] text-slate-500">
                    No questions yet. For example: "The sender says there is no suitable position right now but they will get back later", labelled{" "}
                    <span className="font-medium text-slate-700">Later maybe</span>, holding the contact for 60 days.
                </p>
            )}

            {value.length > 0 && (
                <div className="rounded-md border border-slate-200 divide-y divide-slate-100 bg-white">
                    {value.map((q, i) => (
                        <div
                            key={q.id}
                            role="button"
                            tabIndex={0}
                            onClick={() => open(i)}
                            onKeyDown={(e) => {
                                if (e.key === "Enter") open(i);
                            }}
                            className="group px-3 py-2 flex items-start gap-3 cursor-pointer hover:bg-slate-50 transition-colors"
                        >
                            <div className="min-w-0 flex-1">
                                <p className="text-[12.5px] text-slate-900 leading-snug">{q.question}</p>
                                <div className="mt-1 flex flex-wrap items-center gap-1">
                                    {(q.type === "choice" ? q.choices ?? [] : [{ label: q.label ?? "", description: "", action: q.action }]).map((c) => (
                                        <span
                                            key={c.label}
                                            className="px-1.5 rounded bg-sky-50 text-sky-700 text-[11px]"
                                            title={actionSummary(c.action) || "Labels only"}
                                        >
                                            {c.label}
                                            {actionSummary(c.action) && (
                                                <span className="text-sky-500"> · {actionSummary(c.action)}</span>
                                            )}
                                        </span>
                                    ))}
                                    <span className="text-[11px] text-slate-400">
                                        {q.type === "choice" ? "pick one" : "yes or no"}
                                    </span>
                                </div>
                            </div>
                            <div className="shrink-0 flex items-center gap-0.5">
                                <button
                                    type="button"
                                    aria-label="Edit question"
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        open(i);
                                    }}
                                    className={ICON_BUTTON}
                                >
                                    <PencilIcon className="w-3.5 h-3.5" />
                                </button>
                                <button
                                    type="button"
                                    aria-label="Remove question"
                                    onClick={(e) => {
                                        e.stopPropagation();
                                        remove(i);
                                    }}
                                    className={`${ICON_BUTTON} hover:text-red-600 hover:bg-red-50`}
                                >
                                    <Trash2Icon className="w-3.5 h-3.5" />
                                </button>
                            </div>
                        </div>
                    ))}
                </div>
            )}

            {editing ? (
                <QuestionForm
                    key={editing.draft.id}
                    initial={editing.draft}
                    isNew={editing.index === null}
                    taken={
                        new Set(
                            value
                                .filter((_, i) => i !== editing.index)
                                .flatMap(labelsOf)
                                .map((l) => l.toLowerCase()),
                        )
                    }
                    onCancel={() => setEditing(null)}
                    onSave={(q) => {
                        onChange(
                            editing.index === null
                                ? [...value, q]
                                : value.map((existing, i) => (i === editing.index ? q : existing)),
                        );
                        setEditing(null);
                    }}
                />
            ) : (
                <div className="flex items-center gap-2">
                    <button type="button" className={SECONDARY} disabled={atMax} onClick={() => open(null)}>
                        <PlusIcon className="w-3.5 h-3.5" />
                        Add a question
                    </button>
                    {atMax && (
                        <span className="text-[11.5px] text-slate-500">
                            {INBOX_TAG_QUESTIONS_MAX} is the most a workspace can ask. Each question is sent with every message.
                        </span>
                    )}
                </div>
            )}
        </div>
    );
}

function QuestionForm({
    initial,
    isNew,
    taken,
    onCancel,
    onSave,
}: {
    initial: InboxTagQuestion;
    isNew: boolean;
    taken: Set<string>;
    onCancel: () => void;
    onSave: (q: InboxTagQuestion) => void;
}) {
    const confirm = useConfirm();
    const [q, setQ] = React.useState<InboxTagQuestion>(initial);
    const [tried, setTried] = React.useState(false);
    const dirty = JSON.stringify(q) !== JSON.stringify(initial);
    const issues = problems(q, taken);

    const patch = (next: Partial<InboxTagQuestion>) => setQ((prev) => ({ ...prev, ...next }));
    const patchChoice = (i: number, next: Partial<InboxTagChoice>) =>
        setQ((prev) => ({
            ...prev,
            choices: (prev.choices ?? []).map((c, j) => (j === i ? { ...c, ...next } : c)),
        }));

    const setType = (type: InboxTagQuestion["type"]) => {
        if (type === q.type) return;
        patch(
            type === "choice"
                ? { type, choices: (q.choices ?? []).length ? q.choices : [blankChoice(), blankChoice()] }
                : { type },
        );
    };

    const cancel = () => {
        if (!dirty) return onCancel();
        confirm.show(isNew ? "Discard this question?" : "Discard your changes to this question?", async () => onCancel());
    };

    const save = () => {
        setTried(true);
        if (issues.length === 0) onSave(finalize(q));
    };

    const choices = q.choices ?? [];

    return (
        <div
            className="rounded-md border border-slate-200 bg-slate-50/60 p-3 space-y-3"
            onKeyDown={(e) => {
                if (e.key === "Escape" && !document.querySelector("[data-floating], [role='alertdialog']")) {
                    e.stopPropagation();
                    cancel();
                }
            }}
        >
            <div className="flex items-center gap-1">
                {(
                    [
                        ["yes_no", "Yes or no"],
                        ["choice", "Pick one"],
                    ] as const
                ).map(([type, label]) => (
                    <button
                        key={type}
                        type="button"
                        aria-pressed={q.type === type}
                        onClick={() => setType(type)}
                        className={`h-7 px-2.5 rounded-md border text-[11.5px] transition-colors ${
                            q.type === type
                                ? "bg-sky-50 text-sky-700 border-sky-200"
                                : "bg-white text-slate-500 border-slate-200 hover:border-slate-300"
                        }`}
                    >
                        {label}
                    </button>
                ))}
            </div>

            <div>
                <div className="text-[10px] uppercase tracking-[0.14em] text-slate-500 mb-1">Question</div>
                <TextInput
                    value={q.question}
                    onChange={(v) => patch({ question: v.slice(0, INBOX_TAG_QUESTION_MAX_LEN) })}
                    placeholder={
                        q.type === "yes_no"
                            ? "The sender says there is no suitable position now but they will get back later."
                            : "Who is replying?"
                    }
                    autoFocus={isNew}
                    className="w-full"
                />
                <p className="mt-1 text-[11px] text-slate-500 leading-relaxed">
                    One plain statement about the reply, read literally: no double negatives, one judgment per question. It is sent to TypeSafe with every classified message, in any language your mail is in.
                </p>
            </div>

            {q.type === "yes_no" ? (
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <LabelField value={q.label ?? ""} onChange={(label) => patch({ label })} />
                    <ActionField value={q.action} onChange={(action) => patch({ action })} hint="Acts only at 80% or more." />
                </div>
            ) : (
                <div className="space-y-2">
                    <div className="text-[10px] uppercase tracking-[0.14em] text-slate-500">Options</div>
                    {choices.map((c, i) => (
                        <div key={i} className="rounded-md border border-slate-200 bg-white p-2 space-y-2">
                            <div className="flex items-start gap-2">
                                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 flex-1 min-w-0">
                                    <LabelField value={c.label} onChange={(label) => patchChoice(i, { label })} />
                                    <ActionField value={c.action} onChange={(action) => patchChoice(i, { action })} />
                                </div>
                                {choices.length > INBOX_TAG_CHOICES_MIN && (
                                    <button
                                        type="button"
                                        aria-label={`Remove option ${i + 1}`}
                                        onClick={() => patch({ choices: choices.filter((_, j) => j !== i) })}
                                        className={`${ICON_BUTTON} mt-4`}
                                    >
                                        <XIcon className="w-3.5 h-3.5" />
                                    </button>
                                )}
                            </div>
                            <TextInput
                                value={c.description}
                                onChange={(v) => patchChoice(i, { description: v.slice(0, INBOX_TAG_CHOICE_DESC_MAX_LEN) })}
                                placeholder="When this option applies, in one line"
                                className="w-full"
                            />
                        </div>
                    ))}
                    <div className="flex items-center gap-2">
                        <button
                            type="button"
                            className={SECONDARY}
                            disabled={choices.length >= INBOX_TAG_CHOICES_MAX}
                            onClick={() => patch({ choices: [...choices, blankChoice()] })}
                        >
                            <PlusIcon className="w-3.5 h-3.5" />
                            Add an option
                        </button>
                        <span className="text-[11px] text-slate-500">
                            A reply that fits none of them gets no label. Options act at 70% or more.
                        </span>
                    </div>
                </div>
            )}

            {tried && issues.length > 0 && (
                <ul className="text-[11.5px] text-red-600 space-y-0.5">
                    {issues.map((m) => (
                        <li key={m}>{m}</li>
                    ))}
                </ul>
            )}

            <div className="flex items-center justify-end gap-2">
                <button type="button" className={SECONDARY} onClick={cancel}>
                    Cancel
                </button>
                <button type="button" className={PRIMARY} onClick={save} disabled={tried && issues.length > 0}>
                    {isNew ? "Add question" : "Save question"}
                </button>
            </div>
        </div>
    );
}

function LabelField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
    const name = inboxTagLabelName(value);
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-500 mb-1">Label</div>
            <TextInput value={value} onChange={onChange} placeholder="Later maybe" className="w-full" />
            {value && name !== value.trim() && (
                <p className="mt-1 text-[11px] text-slate-500">
                    Files as <span className="font-medium text-slate-700">{name || "nothing"}</span>
                </p>
            )}
        </div>
    );
}

function ActionField({
    value,
    onChange,
    hint,
}: {
    value: InboxTagQuestionAction;
    onChange: (next: InboxTagQuestionAction) => void;
    hint?: string;
}) {
    return (
        <div>
            <div className="text-[10px] uppercase tracking-[0.14em] text-slate-500 mb-1">Then</div>
            <div className="flex items-center gap-1.5">
                <SelectMenu
                    value={value.type}
                    onChange={(v) => {
                        const type = v as InboxTagActionType;
                        onChange(type === "hold" ? { type, hold_days: value.hold_days ?? INBOX_TAG_HOLD_DEFAULT } : { type });
                    }}
                    options={ACTIONS}
                    aria-label="What a match does"
                    minWidth={170}
                />
                {value.type === "hold" && (
                    <>
                        <NumberInput
                            min={INBOX_TAG_HOLD_MIN}
                            max={INBOX_TAG_HOLD_MAX}
                            value={value.hold_days ?? INBOX_TAG_HOLD_DEFAULT}
                            onChange={(n) =>
                                onChange({
                                    type: "hold",
                                    hold_days: Number.isFinite(n)
                                        ? Math.min(INBOX_TAG_HOLD_MAX, Math.max(INBOX_TAG_HOLD_MIN, n))
                                        : INBOX_TAG_HOLD_DEFAULT,
                                })
                            }
                            className="w-20"
                        />
                        <span className="text-[11.5px] text-slate-500">days</span>
                    </>
                )}
            </div>
            {hint && value.type !== "" && <p className="mt-1 text-[11px] text-slate-500">{hint}</p>}
        </div>
    );
}
