import { PlusIcon, CheckSquareIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function TasksPage() {
    return (
        <Page width="wide">
            <PageHeader title="Tasks" subtitle="Follow-ups and reminders.">
                <button className="flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors">
                    <PlusIcon className="w-3 h-3" />
                    New task
                </button>
            </PageHeader>
            <EmptyState
                icon={<CheckSquareIcon className="w-5 h-5" />}
                title="No tasks"
                description="Add a task when you need to follow up — they'll show up here, on the home page, and on the contact."
            />
        </Page>
    );
}
