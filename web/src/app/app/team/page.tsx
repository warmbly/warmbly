import { PlusIcon, UsersIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function TeamPage() {
    return (
        <Page width="wide">
            <PageHeader
                title="Team"
                subtitle="Manage members and roles for this workspace."
            >
                <button className="bg-sky-600 text-white hover:bg-sky-700 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors flex items-center gap-1.5">
                    <PlusIcon className="w-3.5 h-3.5" />
                    Invite member
                </button>
            </PageHeader>

            <EmptyState
                icon={<UsersIcon className="w-5 h-5" />}
                title="No team members yet"
                description="Invite teammates to collaborate on campaigns, mailboxes, and reporting."
            />
        </Page>
    );
}
