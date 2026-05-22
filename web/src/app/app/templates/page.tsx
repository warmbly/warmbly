import { PlusIcon, FileTextIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function TemplatesPage() {
    return (
        <Page width="wide">
            <PageHeader title="Templates" subtitle="Reusable email and sequence drafts.">
                <button className="flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors">
                    <PlusIcon className="w-3.5 h-3.5" />
                    New template
                </button>
            </PageHeader>
            <EmptyState
                icon={<FileTextIcon className="w-5 h-5" />}
                title="No templates yet"
                description="Save common openers and sequences as templates so you don't rewrite them every time."
            />
        </Page>
    );
}
