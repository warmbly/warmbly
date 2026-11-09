// The codes other warmup services put in their mail: the same chip box as the
// language picker, with a field to add one instead of a dropdown.

import React from "react";
import { PlusIcon, XIcon } from "lucide-react";
import { TextInput } from "@/components/ui/field";
import { EXTERNAL_WARMUP_TAG_MAX, warmupTagProblem } from "@/lib/api/models/app/outreach/OutreachSettings";

export default function WarmupTags({ value, onChange }: { value: string[]; onChange: (next: string[]) => void }) {
    const [draft, setDraft] = React.useState("");
    const problem = warmupTagProblem(draft, value);

    const add = () => {
        const tag = draft.trim();
        if (!tag || problem) return;
        onChange([...value, tag]);
        setDraft("");
    };

    return (
        <div className="w-full sm:w-[360px]">
            <div className="rounded-md border border-slate-200 bg-white min-h-[34px]">
                {value.length === 0 ? (
                    <p className="px-3 py-2 text-[11.5px] text-slate-400">No tags yet</p>
                ) : (
                    <div className="px-2 py-2 flex flex-wrap gap-1">
                        {value.map((tag) => (
                            <span
                                key={tag}
                                className="inline-flex items-center gap-1 h-5 pl-1.5 pr-0.5 rounded bg-sky-50 text-sky-700 text-[11px] max-w-full"
                            >
                                <span className="truncate font-mono">{tag}</span>
                                <button
                                    type="button"
                                    aria-label={`Remove ${tag}`}
                                    onClick={() => onChange(value.filter((t) => t !== tag))}
                                    className="size-4 shrink-0 rounded inline-flex items-center justify-center hover:bg-sky-100"
                                >
                                    <XIcon className="w-2.5 h-2.5" />
                                </button>
                            </span>
                        ))}
                    </div>
                )}
            </div>
            <div className="mt-1.5 flex items-center gap-1.5">
                <TextInput
                    value={draft}
                    onChange={setDraft}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") {
                            e.preventDefault();
                            add();
                        }
                    }}
                    placeholder="Paste a warmup tag"
                    title="Warmup tag"
                    invalid={!!problem}
                    maxLength={EXTERNAL_WARMUP_TAG_MAX}
                    className="flex-1 min-w-0"
                />
                <button
                    type="button"
                    onClick={add}
                    disabled={!draft.trim() || !!problem}
                    className="h-7 px-2.5 inline-flex items-center gap-1 rounded-md border border-slate-200 text-[12px] font-medium text-slate-600 hover:border-slate-300 hover:text-slate-800 disabled:opacity-50 disabled:hover:border-slate-200"
                >
                    <PlusIcon className="w-3 h-3" />
                    Add
                </button>
            </div>
            {problem && <p className="mt-1 text-[11.5px] text-rose-600">{problem}</p>}
        </div>
    );
}
