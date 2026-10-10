// The words under a tour scene: the slide's title and points, and Remie's
// card with the same job handed to it (or to @Warmbly in Slack).

import type { ReactNode } from "react";
import { ArrowRightIcon, CheckIcon, MousePointerClickIcon } from "lucide-react";
import AgentMark from "@/components/app/agent/AgentMark";
import ProviderGlyph from "@/app/app/integrations/_components/ProviderGlyph";
import { cn } from "@/lib/utils";
import type { FlatSlide } from "./tourSteps";
import { EYEBROW, SECONDARY } from "./tourUi";

export function Title({ slide, titleId, lead, title }: { slide: FlatSlide; titleId: string; lead?: string; title?: string }) {
    return (
        <h2 id={titleId} className="text-[22px] sm:text-[26px] font-medium tracking-[-0.035em] leading-[1.12]">
            <span className="text-slate-400">{lead ?? slide.lead} </span>
            <span className="text-slate-900">{title ?? slide.title}</span>
        </h2>
    );
}

export function SlideCopy({
    slide,
    titleId,
    where,
    firstName,
    mobile,
    canAsk,
    onAsk,
    onOpenRemie,
    onConnectSlack,
}: {
    slide: FlatSlide;
    titleId: string;
    where: string | null;
    firstName: string;
    mobile: boolean;
    canAsk: boolean;
    onAsk: (prompt: string) => void;
    onOpenRemie?: () => void;
    onConnectSlack?: () => void;
}) {
    const welcome = slide.scene === "welcome";
    const lead = welcome && firstName ? `Welcome, ${firstName}.` : undefined;
    const count = slide.chapter.slides.length;
    return (
        <div
            className={cn(
                "grid shrink-0",
                mobile ? "grid-cols-1 gap-5 px-5 pt-5 pb-6" : "h-[252px] grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)] gap-10 px-8 pt-6 pb-5",
            )}
        >
            <div className={cn(!mobile && "overflow-y-auto")}>
                <div className={cn(EYEBROW, "flex items-center gap-2")}>
                    <span>
                        {slide.chapter.name}
                        {count > 1 && ` · ${slide.si + 1} of ${count}`}
                    </span>
                    {where && (
                        <>
                            <span className="h-px w-3 bg-slate-300" />
                            <span>
                                {where}: {slide.label}
                            </span>
                        </>
                    )}
                </div>
                <div className="mt-3">
                    <Title slide={slide} titleId={titleId} lead={lead} />
                </div>
                <p className="mt-3 text-[13.5px] leading-[1.6] text-slate-500">{slide.body}</p>
            </div>

            <div className={cn("flex flex-col gap-4", !mobile && "overflow-y-auto")}>
                {welcome ? (
                    <ul className="space-y-1.5">
                        {[
                            { icon: <MousePointerClickIcon className="size-3.5 text-slate-600" />, text: slide.points[0] },
                            { icon: <AgentMark size={16} />, text: slide.points[1] },
                            { icon: <ProviderGlyph provider="slack" name="Slack" size={7} />, text: slide.points[2] },
                        ].map((p) => (
                            <li key={p.text} className="flex items-center gap-3 rounded-[10px] bg-slate-50 px-3 h-10 ring-1 ring-slate-200/70">
                                <span className="size-6 rounded-md bg-white ring-1 ring-slate-200 inline-flex items-center justify-center shrink-0 overflow-hidden">{p.icon}</span>
                                <span className="text-[13px] text-slate-800">{p.text}</span>
                            </li>
                        ))}
                    </ul>
                ) : (
                    <ul className="space-y-1.5">
                        {slide.points.map((p) => (
                            <li key={p} className="flex items-start gap-2.5 text-[13px] leading-[1.45] text-slate-700">
                                <span className="mt-[1px] size-4 rounded-full bg-slate-100 inline-flex items-center justify-center shrink-0">
                                    <CheckIcon className="size-2.5 text-slate-600" strokeWidth={3} />
                                </span>
                                {p}
                            </li>
                        ))}
                    </ul>
                )}
                <RemieCard slide={slide} canAsk={canAsk} onAsk={onAsk} onOpenRemie={onOpenRemie} onConnectSlack={onConnectSlack} />
            </div>
        </div>
    );
}

// Remie's line on this slide, and the same job handed to it (or to @Warmbly in Slack).
export function RemieCard({
    slide,
    canAsk,
    onAsk,
    onOpenRemie,
    onConnectSlack,
}: {
    slide: FlatSlide;
    canAsk: boolean;
    onAsk: (prompt: string) => void;
    onOpenRemie?: () => void;
    onConnectSlack?: () => void;
}) {
    const line = slide.remie ?? "Rather not click through it yourself? Just ask me.";
    const ask = slide.ask;
    const prompt: ReactNode = ask && (
        <>
            <span className="min-w-0 flex-1 line-clamp-2">“{ask}”</span>
            {canAsk && (
                <span className="shrink-0 inline-flex items-center gap-1 text-[11.5px] font-medium text-sky-700">
                    Ask <ArrowRightIcon className="size-3" />
                </span>
            )}
        </>
    );
    const promptCls = "mt-2.5 flex w-full items-center gap-2 rounded-[9px] bg-white px-3 py-2 min-h-9 text-left leading-[1.4] text-[12.5px] text-slate-800 ring-1 ring-slate-200";
    return (
        <div className="rounded-[12px] bg-gradient-to-br from-sky-50/80 to-slate-50 p-3.5 ring-1 ring-slate-200/70">
            <div className="flex items-start gap-2.5">
                <span className="mt-0.5 shrink-0">
                    <AgentMark size={20} />
                </span>
                <p className="text-[12.5px] leading-[1.5] text-slate-700">
                    <span className="font-medium text-slate-900">Remie </span>
                    {line}
                </p>
            </div>
            {ask &&
                (canAsk ? (
                    <button
                        type="button"
                        onClick={() => onAsk(ask)}
                        className={cn(promptCls, "transition-colors hover:ring-sky-300 hover:bg-sky-50/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400")}
                    >
                        {prompt}
                    </button>
                ) : (
                    <div className={promptCls}>{prompt}</div>
                ))}
            {slide.slack && (
                <div className={promptCls}>
                    <ProviderGlyph provider="slack" name="Slack" size={7} />
                    <span className="min-w-0 flex-1 line-clamp-2">{slide.slack}</span>
                </div>
            )}
            {(onOpenRemie || onConnectSlack) && (
                <div className="mt-2.5 flex flex-wrap gap-2">
                    {onOpenRemie && (
                        <button type="button" onClick={onOpenRemie} className={cn(SECONDARY, "h-8 px-3 text-[12.5px]")}>
                            <AgentMark size={14} /> Open Remie now
                        </button>
                    )}
                    {onConnectSlack && (
                        <button type="button" onClick={onConnectSlack} className={cn(SECONDARY, "h-8 px-3 text-[12.5px]")}>
                            <ProviderGlyph provider="slack" name="Slack" size={7} /> Connect Slack
                        </button>
                    )}
                </div>
            )}
            {!canAsk && (ask || slide.scene === "remieAct") && (
                <p className="mt-2 text-[11.5px] leading-[1.45] text-slate-400">Remie comes with every paid plan and runs on AI credits.</p>
            )}
        </div>
    );
}
