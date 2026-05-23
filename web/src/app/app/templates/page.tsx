// Templates — gallery preview.
//
// The page is empty for now (no backend yet) but it shouldn't look
// like every other "coming soon" page. Instead we show a faux
// gallery of sample template cards so the user gets a sense of what
// templates look like before they build any.

import { PlusIcon, MailOpenIcon, ReplyIcon, MousePointerClickIcon } from "lucide-react";
import {
    Page,
    PageBody,
    PageTopbar,
    SectionBar,
    Stat,
    StatStrip,
    TopbarAction,
} from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

const SAMPLE_TEMPLATES = [
    {
        name: "Cold intro · Product",
        subject: "Quick question, {{first_name}}",
        preview:
            "Hi {{first_name}}, I noticed {{company}} just shipped {{recent_announcement}}…",
        tag: "Sales",
        used: 12,
    },
    {
        name: "Follow-up · After 3 days",
        subject: "Re: Quick question",
        preview:
            "Hey {{first_name}} — circling back on this. Worth a 15-min chat next Tue?",
        tag: "Sales",
        used: 8,
    },
    {
        name: "Re-engagement · 30 days",
        subject: "Still on your radar?",
        preview:
            "It's been a while — wanted to share a couple of new things at {{our_company}}…",
        tag: "Nurture",
        used: 4,
    },
    {
        name: "Meeting confirm",
        subject: "Looking forward to {{date}}",
        preview:
            "Just confirming our {{day}} call at {{time}}. Calendar invite attached.",
        tag: "Ops",
        used: 21,
    },
    {
        name: "Resource share",
        subject: "Thought you'd find this useful",
        preview:
            "Pulled together a short breakdown of {{topic}} you mentioned…",
        tag: "Nurture",
        used: 3,
    },
    {
        name: "Renewal nudge",
        subject: "Two weeks left on your plan",
        preview:
            "Quick heads up — your {{plan}} plan renews on {{date}}. Want to upgrade?",
        tag: "Ops",
        used: 6,
    },
];

export default function TemplatesPage() {
    return (
        <Page>
            <PageTopbar
                eyebrow="Templates"
                subtitle="Reusable subject + body for cold opens, follow-ups, replies"
            >
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Templates")}
                >
                    New template
                </TopbarAction>
            </PageTopbar>

            <StatStrip cols={4}>
                <Stat label="Saved" value={0} sub="reusable drafts" />
                <Stat label="Used this week" value={0} sub="campaign attaches" accent={false} />
                <Stat label="Avg open" value="—%" sub="across templates" />
                <Stat label="Avg reply" value="—%" sub="across templates" last />
            </StatStrip>

            <SectionBar label="Gallery preview" />
            <PageBody className="px-5 py-5">
                <div className="rounded-md border border-dashed border-slate-300 bg-slate-50/40 p-4 mb-4">
                    <p className="text-[12px] text-slate-700 leading-relaxed">
                        Templates aren't shipped yet — here's a peek at the shape.
                        Each card holds a subject, a preview, an audience tag and
                        a usage counter, so you can find the right one fast from
                        any campaign.
                    </p>
                </div>

                <div
                    aria-hidden
                    className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 opacity-70 pointer-events-none select-none"
                >
                    {SAMPLE_TEMPLATES.map((t, i) => (
                        <div
                            key={i}
                            className="rounded-md border border-slate-200 bg-white p-3 flex flex-col gap-2"
                        >
                            <div className="flex items-center gap-1.5">
                                <span className="text-[10px] uppercase tracking-[0.1em] font-medium text-slate-500 bg-slate-100 rounded-sm px-1 py-px">
                                    {t.tag}
                                </span>
                                <span className="ml-auto font-mono text-[10px] text-slate-400 tabular-nums">
                                    {t.used} uses
                                </span>
                            </div>
                            <div className="text-[12.5px] font-semibold text-slate-900 leading-snug">
                                {t.name}
                            </div>
                            <div className="text-[11.5px] text-slate-600 truncate">
                                {t.subject}
                            </div>
                            <div className="text-[11px] text-slate-400 leading-relaxed line-clamp-3">
                                {t.preview}
                            </div>
                            <div className="mt-1 flex items-center gap-3 text-[10.5px] text-slate-400 font-mono">
                                <span className="inline-flex items-center gap-1">
                                    <MailOpenIcon className="w-2.5 h-2.5" />
                                    —%
                                </span>
                                <span className="inline-flex items-center gap-1">
                                    <MousePointerClickIcon className="w-2.5 h-2.5" />
                                    —%
                                </span>
                                <span className="inline-flex items-center gap-1">
                                    <ReplyIcon className="w-2.5 h-2.5" />
                                    —%
                                </span>
                            </div>
                        </div>
                    ))}
                </div>
            </PageBody>
        </Page>
    );
}
