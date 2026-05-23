import { PlusIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, TopbarAction } from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

export default function DealsPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Deals" subtitle="Opportunities by stage">
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Deals")}
                >
                    New deal
                </TopbarAction>
            </PageTopbar>
            <PageBody>
                <EmptyBlock
                    title="No deals"
                    body="Create your first deal to start tracking opportunities."
                />
            </PageBody>
        </Page>
    );
}
