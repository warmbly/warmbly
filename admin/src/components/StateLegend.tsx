// Inline "what do these states mean?" legend for enum badges (risk pools,
// health states, job statuses). Renders a small help trigger that reveals a
// term → definition list on hover/focus, so tables stay compact but no state
// name is ever left unexplained.

import { CircleHelp } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from "@/components/ui/tooltip";

export interface LegendEntry {
    term: string;
    description: string;
    /** Badge tone classes, e.g. "border-emerald-300 bg-emerald-50 text-emerald-700". */
    tone?: string;
}

export function StateLegend({
    label = "What do these states mean?",
    entries,
}: {
    label?: string;
    entries: LegendEntry[];
}) {
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <button
                    type="button"
                    className="inline-flex items-center gap-1 text-[11px] text-muted-foreground underline decoration-dotted underline-offset-2 hover:text-foreground"
                >
                    <CircleHelp className="size-3.5" />
                    {label}
                </button>
            </TooltipTrigger>
            <TooltipContent
                side="bottom"
                align="start"
                className="max-w-sm bg-popover text-popover-foreground border border-border shadow-md"
            >
                <dl className="space-y-1.5 py-0.5">
                    {entries.map((e) => (
                        <div key={e.term} className="flex items-start gap-2">
                            <dt className="shrink-0">
                                <Badge
                                    variant="outline"
                                    className={`text-[10px] ${e.tone ?? "border-zinc-300 text-zinc-700"}`}
                                >
                                    {e.term}
                                </Badge>
                            </dt>
                            <dd className="text-[11px] leading-snug text-muted-foreground">
                                {e.description}
                            </dd>
                        </div>
                    ))}
                </dl>
            </TooltipContent>
        </Tooltip>
    );
}
