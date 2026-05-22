import { PlusIcon, CircleDollarSignIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function DealsPage() {
    return (
        <Page width="wide">
            <PageHeader title="Deals" subtitle="Opportunities by stage.">
                <button className="bg-sky-600 text-white hover:bg-sky-700 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors flex items-center gap-1.5">
                    <PlusIcon className="w-3.5 h-3.5" />
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
