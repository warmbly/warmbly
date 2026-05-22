import { PlusIcon, CircleDollarSignIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function DealsPage() {
    return (
        <Page width="wide">
            <PageHeader title="Deals" subtitle="Opportunities by stage.">
                <button className="flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors">
                    <PlusIcon className="w-3 h-3" />
                    New deal
                </button>
            </PageHeader>
            <EmptyState
                icon={<CircleDollarSignIcon className="w-5 h-5" />}
                title="No deals"
                description="Create your first deal to start tracking opportunities."
            />
        </Page>
    );
}
