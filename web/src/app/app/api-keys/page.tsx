import { PlusIcon, KeyIcon } from "lucide-react";
import { EmptyState, Page, PageHeader } from "@/components/layout/Page";

export default function APIKeysPage() {
    return (
        <Page width="wide">
            <PageHeader title="API keys" subtitle="Issue and revoke programmatic access.">
                <button className="flex items-center gap-1.5 h-7 px-2.5 rounded-md bg-slate-900 text-white hover:bg-slate-800 text-[12.5px] font-medium transition-colors">
                    <PlusIcon className="w-3 h-3" />
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
