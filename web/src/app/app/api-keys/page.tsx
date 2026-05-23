import { PlusIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, TopbarAction } from "@/components/layout/Page";

export default function APIKeysPage() {
    return (
        <Page>
            <PageTopbar eyebrow="API keys" subtitle="Programmatic access tokens">
                <TopbarAction icon={<PlusIcon className="w-3 h-3" />}>Create key</TopbarAction>
            </PageTopbar>
            <PageBody>
                <EmptyBlock
                    title="No keys yet"
                    body="Create an API key to integrate Warmbly with your own tools."
                />
            </PageBody>
        </Page>
    );
}
