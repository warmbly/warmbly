import { SparklesIcon, ArrowUpRightIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { Page, PageBody, PageTopbar, SectionBar, TopbarAction } from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

export default function BillingPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Billing" subtitle="Subscription and invoices" />

            <SectionBar label="Current plan" />
            <div className="px-5 py-4 flex items-center gap-4 border-b border-slate-200/60">
                <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                        <span className="text-[13px] font-medium text-slate-900">Free</span>
                        <span className="inline-flex items-center text-[10px] rounded px-1.5 h-4 bg-slate-100 text-slate-500 uppercase tracking-[0.1em] font-medium">
                            current
                        </span>
                    </div>
                    <p className="text-[11.5px] text-slate-500 mt-0.5">Basic features for getting started.</p>
                </div>
                <TopbarAction
                    icon={<SparklesIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Upgrade flow")}
                >
                    Upgrade
                </TopbarAction>
            </div>
            <PageBody>
                <div className="px-5 py-3">
                    <Link
                        to="/#pricing"
                        className="inline-flex items-center gap-1 text-[11px] text-slate-500 hover:text-slate-900 transition-colors"
                    >
                        <ArrowUpRightIcon className="w-3 h-3" />
                        <span>View all plans and pricing</span>
                    </Link>
                </div>
            </PageBody>
        </Page>
    );
}
