// "Write with AI" — a compact popover that drafts email copy from a short
// prompt + tone and hands the result back to the composer. Surfaces remaining
// generation credits and turns a 402 (out of credits) into a friendly toast.
//
// The component is deliberately editor-agnostic: it calls `onInsert(text)` with
// the generated draft, so the host (SequenceView) decides whether to drop it
// into the subject, the body editor, etc.

import React from "react";
import { SparklesIcon, Loader2Icon, WandSparklesIcon } from "lucide-react";
import toast from "react-hot-toast";
import {
    PopoverMenu,
    PopoverMenuContent,
    PopoverMenuTrigger,
} from "@/components/ui/popover-menu";
import { Label } from "@/components/ui/field";
import useGenerateWrite from "@/lib/api/hooks/app/generation/useGenerateWrite";
import { WRITE_TONES } from "@/lib/api/models/app/generation/Write";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function WriteWithAI({ onInsert }: { onInsert: (text: string) => void }) {
    const generate = useGenerateWrite();
    const [open, setOpen] = React.useState(false);
    const [prompt, setPrompt] = React.useState("");
    const [tone, setTone] = React.useState("");
    // Remembered from the last successful generation so we can show a running
    // credit balance without a separate fetch.
    const [credits, setCredits] = React.useState<number | null>(null);

    const run = () => {
        const text = prompt.trim();
        if (!text || generate.isPending) return;
        generate.mutate(
            { prompt: text, tone: tone || undefined },
            {
                onSuccess: (res) => {
                    setCredits(res.credits_remaining);
                    onInsert(res.text);
                    toast.success(
                        `Draft inserted · ${res.credits_remaining} credit${res.credits_remaining === 1 ? "" : "s"} left`,
                    );
                    setPrompt("");
                    setOpen(false);
                },
                onError: (e) => {
                    const err = e as unknown as AppError;
                    if (err?.status === 402) {
                        toast.error(
                            "You're out of AI writing credits. Upgrade your plan or wait for your credits to refresh.",
                        );
                        return;
                    }
                    toast.error(buildError(err));
                },
            },
        );
    };

    return (
        <PopoverMenu open={open} onOpenChange={setOpen} align="end">
            <PopoverMenuTrigger asChild>
                <button
                    type="button"
                    title="Draft this email with AI"
                    className="h-7 px-2.5 inline-flex items-center gap-1.5 rounded-md border border-sky-200 bg-sky-50 text-[12px] font-medium text-sky-700 transition-colors hover:border-sky-300 hover:bg-sky-100"
                >
                    <SparklesIcon className="w-3.5 h-3.5" />
                    Write with AI
                </button>
            </PopoverMenuTrigger>
            <PopoverMenuContent minWidth={320} className="p-3">
                <div className="space-y-3">
                    <div>
                        <Label>What should this email say?</Label>
                        <textarea
                            autoFocus
                            value={prompt}
                            onChange={(e) => setPrompt(e.target.value)}
                            onKeyDown={(e) => {
                                if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                                    e.preventDefault();
                                    run();
                                }
                            }}
                            placeholder="e.g. A short intro asking to book a 15-min call about cutting their AWS bill."
                            rows={3}
                            className="w-full resize-none rounded-md border border-slate-200 bg-white px-2.5 py-2 text-[12.5px] text-slate-900 placeholder:text-slate-400 outline-none transition-colors focus:border-sky-400 focus:ring-2 focus:ring-sky-100"
                        />
                    </div>

                    <div>
                        <Label>Tone</Label>
                        <div className="flex flex-wrap gap-1.5">
                            {WRITE_TONES.map((t) => (
                                <button
                                    key={t.value || "default"}
                                    type="button"
                                    onClick={() => setTone(t.value)}
                                    className={`h-6 rounded-md border px-2 text-[11.5px] font-medium transition-colors ${
                                        tone === t.value
                                            ? "border-sky-300 bg-sky-50 text-sky-700"
                                            : "border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:text-slate-900"
                                    }`}
                                >
                                    {t.label}
                                </button>
                            ))}
                        </div>
                    </div>

                    <div className="flex items-center justify-between gap-2 pt-0.5">
                        <span className="text-[10.5px] text-slate-400">
                            {credits === null
                                ? "Drafts use AI writing credits."
                                : `${credits} credit${credits === 1 ? "" : "s"} remaining`}
                        </span>
                        <button
                            type="button"
                            onClick={run}
                            disabled={!prompt.trim() || generate.isPending}
                            className="h-7 px-3 inline-flex items-center gap-1.5 rounded-md bg-sky-600 text-[12px] font-medium text-white transition-colors hover:bg-sky-700 disabled:opacity-50"
                        >
                            {generate.isPending ? (
                                <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                            ) : (
                                <WandSparklesIcon className="w-3.5 h-3.5" />
                            )}
                            Generate
                        </button>
                    </div>
                </div>
            </PopoverMenuContent>
        </PopoverMenu>
    );
}
