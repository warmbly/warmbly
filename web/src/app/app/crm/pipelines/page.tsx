import { PlusIcon, GitBranchIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function PipelinesPage() {
    return (
        <Page width="wide">
            <PageHeader title="Pipelines" subtitle="The stages a deal moves through.">
                <button className="bg-sky-600 text-white hover:bg-sky-700 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors flex items-center gap-1.5">
                    <PlusIcon className="w-3.5 h-3.5" />
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
