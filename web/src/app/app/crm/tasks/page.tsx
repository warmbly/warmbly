import { PlusIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, TopbarAction } from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

export default function TasksPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Tasks" subtitle="Follow-ups and reminders">
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Tasks")}
                >
                    New task
                </TopbarAction>
            </PageTopbar>
            <PageBody>
                <EmptyBlock
                    title="No tasks"
                    body="Add a task when you need to follow up — they'll show up here and on the contact."
                />
            </PageBody>
        </Page>
    );
}
