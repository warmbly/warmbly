import { PlusIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, TopbarAction } from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

export default function TeamPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Team" subtitle="Members and roles in this workspace">
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Team invites")}
                >
                    Invite member
                </TopbarAction>
            </PageTopbar>
            <PageBody>
                <EmptyBlock
                    title="No team members yet"
                    body="Invite teammates to collaborate on campaigns, mailboxes, and reporting."
                    cta={
                        <TopbarAction
                            icon={<PlusIcon className="w-3 h-3" />}
                            onClick={() => comingSoon("Team invites")}
                        >
                            Invite member
                        </TopbarAction>
                    }
                />
            </PageBody>
        </Page>
    );
}
