import { PlusIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, TopbarAction } from "@/components/layout/Page";

export default function TeamPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Team" subtitle="Members and roles in this workspace">
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />}>Invite member</TopbarAction>
            </PageTopbar>
            <PageBody>
                <EmptyBlock
                    title="No team members yet"
                    body="Invite teammates to collaborate on campaigns, mailboxes, and reporting."
                />
            </PageBody>
        </Page>
    );
}
