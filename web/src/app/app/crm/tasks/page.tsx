import { PlusIcon, CheckSquareIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function TasksPage() {
    return (
        <Page width="wide">
            <PageHeader title="Tasks" subtitle="Follow-ups and reminders.">
                <button className="bg-sky-600 text-white hover:bg-sky-700 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors flex items-center gap-1.5">
                    <PlusIcon className="w-3.5 h-3.5" />
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
