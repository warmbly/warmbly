import { PlusIcon } from "lucide-react";
import { EmptyBlock, Page, PageBody, PageTopbar, TopbarAction } from "@/components/layout/Page";
import { comingSoon } from "@/lib/helper/comingSoon";

export default function TemplatesPage() {
    return (
        <Page>
            <PageTopbar eyebrow="Templates" subtitle="Reusable email and sequence drafts">
                <TopbarAction
                    icon={<PlusIcon className="w-3 h-3" />}
                    onClick={() => comingSoon("Templates")}
                >
                    New template
                </TopbarAction>
            </PageTopbar>
            <PageBody>
                <EmptyBlock
                    title="No templates yet"
                    body="Save common openers and sequences so you don't rewrite them every time."
                    cta={
                        <TopbarAction
                            icon={<PlusIcon className="w-3 h-3" />}
                            onClick={() => comingSoon("Templates")}
                        >
                            New template
                        </TopbarAction>
                    }
                />
            </PageBody>
        </Page>
    );
}
