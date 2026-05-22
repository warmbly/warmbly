import { PlusIcon, GitBranchIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function PipelinesPage() {
    return (
        <Page width="wide">
            <PageHeader title="Pipelines" subtitle="The stages a deal moves through.">
                <button className="flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors">
                    <PlusIcon className="w-3 h-3" />
                    New pipeline
                </button>
            </PageHeader>
            <EmptyState
                icon={<GitBranchIcon className="w-5 h-5" />}
                title="No pipelines"
                description="Define your sales stages so deals have somewhere to live."
            />
        </Page>
    );
}
