import { PlusIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, TopbarAction } from "@/components/layout/Page";

export default function PipelinesPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Pipelines" subtitle="Stages a deal moves through">
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />}>New pipeline</TopbarAction>
            </PageTopbar>
            <PageBody>
                <EmptyBlock
                    title="No pipelines"
                    body="Define your sales stages so deals have somewhere to live."
                />
            </PageBody>
        </Page>
    );
}
