import { PlusIcon, UsersIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function TeamPage() {
    return (
        <Page width="wide">
            <PageHeader
                title="Team"
                subtitle="Manage members and roles for this workspace."
            >
                <button className="flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors">
                    <PlusIcon className="w-3 h-3" />
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
