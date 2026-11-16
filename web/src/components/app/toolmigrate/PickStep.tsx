// Step one: which tool the data is coming from.

import { Loader2Icon } from "lucide-react";
import { TOOL_MIGRATION_KINDS, type ToolProviderInfo } from "@/lib/api/models/app/toolmigrate/ToolMigration";
import { cn } from "@/lib/utils";
import { KIND_LABEL } from "./labels";
import { ToolGlyph } from "./parts";

export default function PickStep({
    providers,
    loading,
    error,
    selected,
    onPick,
}: {
    providers: ToolProviderInfo[];
    loading: boolean;
    error: string | null;
    selected: string | null;
    onPick: (id: string) => void;
}) {
    return (
        <div className="p-4 sm:p-5 space-y-3">
            <div>
                <h3 className="text-[13px] font-semibold text-slate-900">Which tool are you moving from?</h3>
                <p className="text-[12px] text-slate-500 mt-0.5 leading-relaxed">
                    Warmbly reads your account with an API key and shows what it found before anything is brought over.
                </p>
            </div>
            {loading ? (
                <div className="h-40 flex items-center justify-center text-[12px] text-slate-400 gap-2">
                    <Loader2Icon className="w-3.5 h-3.5 animate-spin" />
                    Loading tools
                </div>
            ) : error ? (
                <p className="text-[12px] text-red-600">{error}</p>
            ) : (
                <div className="grid grid-cols-1 min-[420px]:grid-cols-2 md:grid-cols-3 gap-2">
                    {providers.map((p) => {
                        const kinds = TOOL_MIGRATION_KINDS.filter((k) => p.kinds.includes(k));
                        return (
                            <button
                                key={p.id}
                                type="button"
                                onClick={() => onPick(p.id)}
                                aria-pressed={selected === p.id}
                                className={cn(
                                    "text-left rounded-md border p-2.5 flex items-start gap-2.5 transition-colors outline-none focus-visible:ring-2 focus-visible:ring-sky-100",
                                    selected === p.id ? "border-sky-400 bg-sky-50/60" : "border-slate-200 hover:border-slate-300 hover:bg-slate-50/60",
                                )}
                            >
                                <ToolGlyph provider={p.id} label={p.label} size={9} />
                                <span className="min-w-0 flex-1">
                                    <span className="block text-[12.5px] font-medium text-slate-900 truncate">{p.label}</span>
                                    <span className="block text-[11px] text-slate-500 leading-snug mt-0.5">
                                        {kinds.length ? kinds.map((k) => KIND_LABEL[k]).join(", ") : "Nothing through its API"}
                                    </span>
                                </span>
                            </button>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
