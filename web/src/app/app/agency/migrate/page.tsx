// Agency > Migrate: bring client accounts over from Smartlead, Instantly and
// similar tools, each source workspace into a client workspace of its own.

import React from "react";
import { ArrowRightLeftIcon } from "lucide-react";
import { AgencyPage } from "@/components/app/agency/AgencyPage";
import { EmptyBlock, PageBody, SectionBar, TopbarAction } from "@/components/layout/Page";
import ToolMigrationWizard from "@/components/app/toolmigrate/ToolMigrationWizard";
import ToolMigrationHistory from "@/components/app/toolmigrate/ToolMigrationHistory";
import { ToolGlyph } from "@/components/app/toolmigrate/parts";
import { useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";
import { useToolMigrationProviders } from "@/lib/api/hooks/app/toolmigrate/useToolMigrations";
import type { ToolMigrationScope } from "@/lib/api/models/app/toolmigrate/ToolMigration";

export default function AgencyMigratePage() {
    const { agency } = useCurrentAgency();
    // The agency shell renders this only once there is an agency.
    if (!agency) return null;
    return <AgencyMigrate agencyId={agency.id} canManage={agency.role === "owner" || agency.role === "admin"} />;
}

function AgencyMigrate({ agencyId, canManage }: { agencyId: string; canManage: boolean }) {
    const scope = React.useMemo<ToolMigrationScope>(() => ({ kind: "agency", agencyId }), [agencyId]);
    const providers = useToolMigrationProviders(scope);
    const [open, setOpen] = React.useState(false);
    const [openId, setOpenId] = React.useState<string | null>(null);
    const [tool, setTool] = React.useState<string | null>(null);
    const tools = providers.data?.data ?? [];

    const startNew = (provider: string | null = null) => {
        setOpenId(null);
        setTool(provider);
        setOpen(true);
    };

    return (
        <AgencyPage
            title="Migrate"
            actions={
                canManage && (
                    <TopbarAction icon={<ArrowRightLeftIcon className="w-3 h-3" />} onClick={() => startNew()}>
                        Start a migration
                    </TopbarAction>
                )
            }
        >
            <PageBody>
                <div className="px-5 py-4 border-b border-slate-200/60 space-y-3">
                    <p className="text-[12.5px] text-slate-600 leading-relaxed max-w-[72ch]">
                        Bring every client over from another outreach tool in one pass. Each source workspace, such as a Smartlead client or an Instantly
                        workspace key, lands in a client workspace you choose or a new one named after it. Campaigns arrive as drafts and never launch on
                        their own, so pause them in the old tool before you launch them here.
                    </p>
                    {canManage && tools.length > 0 && (
                        <div className="flex flex-wrap items-center gap-1.5">
                            {tools.map((t) => (
                                <button
                                    key={t.id}
                                    type="button"
                                    onClick={() => startNew(t.id)}
                                    title={`Migrate from ${t.label}`}
                                    className="h-7 pl-1 pr-2 rounded-md border border-slate-200 hover:border-slate-300 bg-white inline-flex items-center gap-1.5 text-[12px] text-slate-700 transition-colors"
                                >
                                    <ToolGlyph provider={t.id} label={t.label} size={5} />
                                    {t.label}
                                </button>
                            ))}
                        </div>
                    )}
                </div>
                <SectionBar label="Migrations" />
                <ToolMigrationHistory
                    scope={scope}
                    onOpen={(id) => {
                        setOpenId(id);
                        setTool(null);
                        setOpen(true);
                    }}
                    empty={
                        <EmptyBlock
                            title="No migrations yet"
                            body={canManage ? "Start one to see what your old tool holds before anything comes over." : "Agency owners and admins can start one."}
                            cta={canManage ? <TopbarAction onClick={() => startNew()}>Start a migration</TopbarAction> : undefined}
                        />
                    }
                />
            </PageBody>
            <ToolMigrationWizard scope={scope} open={open} onClose={() => setOpen(false)} migrationId={openId} provider={tool} />
        </AgencyPage>
    );
}
