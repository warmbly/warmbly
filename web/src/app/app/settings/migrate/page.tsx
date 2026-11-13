// Settings > Migrate from another tool: bring mailboxes, campaigns, leads and
// the blocklist over from Smartlead, Instantly and similar tools.

import React from "react";
import { ArrowRightLeftIcon } from "lucide-react";
import { usePermission } from "@/hooks/usePermission";
import ToolMigrationWizard from "@/components/app/toolmigrate/ToolMigrationWizard";
import ToolMigrationHistory from "@/components/app/toolmigrate/ToolMigrationHistory";
import { ToolGlyph } from "@/components/app/toolmigrate/parts";
import { useToolMigrationProviders } from "@/lib/api/hooks/app/toolmigrate/useToolMigrations";
import type { ToolMigrationScope } from "@/lib/api/models/app/toolmigrate/ToolMigration";
import { Section, SectionShell } from "../_components/SectionShell";

const SCOPE: ToolMigrationScope = { kind: "workspace" };

export default function MigrateSettingsPage() {
    const canManage = usePermission("MANAGE_CONTACTS");
    const providers = useToolMigrationProviders(SCOPE, canManage);
    const [open, setOpen] = React.useState(false);
    const [openId, setOpenId] = React.useState<string | null>(null);
    const [tool, setTool] = React.useState<string | null>(null);

    const startNew = (provider: string | null = null) => {
        setOpenId(null);
        setTool(provider);
        setOpen(true);
    };

    if (!canManage) {
        return (
            <SectionShell title="Migrate from another tool" description="Bring your mailboxes, campaigns, leads and blocklist over from another outreach tool.">
                <Section eyebrow="Restricted">
                    <p className="text-[12.5px] text-slate-500 leading-relaxed">
                        Migrating needs permission to manage contacts. Bringing campaigns needs permission to manage campaigns, and mailboxes permission to manage mailboxes.
                    </p>
                </Section>
            </SectionShell>
        );
    }

    const tools = providers.data?.data ?? [];

    return (
        <SectionShell
            title="Migrate from another tool"
            description="Bring your mailboxes, campaigns, leads and blocklist over from another outreach tool."
            actions={
                <button
                    type="button"
                    onClick={() => startNew()}
                    className="h-7 px-2.5 rounded-md bg-sky-600 hover:bg-sky-700 text-white text-[12px] font-medium inline-flex items-center gap-1.5 transition-colors"
                >
                    <ArrowRightLeftIcon className="w-3 h-3" />
                    Start a migration
                </button>
            }
        >
            <Section
                eyebrow="How it works"
                description="Paste an API key, see what Warmbly found, choose what comes over. Campaigns arrive as drafts and never launch on their own, so pause them in the old tool before you launch them here."
            >
                {tools.length > 0 && (
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
            </Section>
            <section>
                <div className="px-4 md:px-8 pt-5 pb-2">
                    <h3 className="text-[12.5px] font-semibold text-slate-900 tracking-tight">Migrations</h3>
                </div>
                <div className="md:px-3 border-t border-slate-200/60">
                    <ToolMigrationHistory
                        scope={SCOPE}
                        onOpen={(id) => {
                            setOpenId(id);
                            setTool(null);
                            setOpen(true);
                        }}
                        empty={<p className="px-4 md:px-5 py-6 text-[12px] text-slate-500">No migrations yet. Start one to see what your old tool holds before anything comes over.</p>}
                    />
                </div>
            </section>
            <ToolMigrationWizard scope={SCOPE} open={open} onClose={() => setOpen(false)} migrationId={openId} provider={tool} />
        </SectionShell>
    );
}
