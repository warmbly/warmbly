import { PlusIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, TopbarAction } from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

export default function APIKeysPage() {
    return (
        <Page>
            <PageTopbar eyebrow="API keys" subtitle="Programmatic access tokens">
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("API keys")}
                >
                    Create key
                </TopbarAction>
            </PageTopbar>
            <PageBody>
                <EmptyBlock
                    title="No keys yet"
                    body="Create an API key to integrate Warmbly with your own tools."
                    cta={
                        <TopbarAction
                            icon={<PlusIcon className="w-3 h-3" />}
                            onClick={() => comingSoon("API keys")}
                        >
                            Create key
                        </TopbarAction>
                    }
                />
            </PageBody>
        </Page>
    );
}
