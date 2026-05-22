import { PlusIcon, KeyIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function APIKeysPage() {
    return (
        <Page width="wide">
            <PageHeader title="API keys" subtitle="Issue and revoke programmatic access.">
                <button className="bg-sky-600 text-white hover:bg-sky-700 rounded-lg px-3 py-1.5 text-[13px] font-medium transition-colors flex items-center gap-1.5">
                    <PlusIcon className="w-3.5 h-3.5" />
                    Create key
                </button>
            </PageHeader>
            <EmptyState
                icon={<KeyIcon className="w-5 h-5" />}
                title="No keys yet"
                description="Create an API key to integrate Warmbly with your own tools."
            />
        </Page>
    );
}
