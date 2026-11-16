// Small pieces shared by the migration list and wizard.

import React from "react";
import ProviderGlyph from "@/app/app/integrations/_components/ProviderGlyph";
import type { GlyphSize } from "@/app/app/integrations/_components/glyphSize";
import type { ToolMigrationStatus } from "@/lib/api/models/app/toolmigrate/ToolMigration";
import { cn } from "@/lib/utils";
import { STATUS } from "./labels";

export function Pill({ tone, children, className }: { tone: string; children: React.ReactNode; className?: string }) {
    return (
        <span className={cn("inline-flex items-center h-5 px-1.5 rounded text-[10.5px] font-medium whitespace-nowrap", tone, className)}>
            {children}
        </span>
    );
}

export function StatusPill({ status }: { status: ToolMigrationStatus }) {
    const s = STATUS[status] ?? { label: status, tone: "bg-slate-100 text-slate-500" };
    return <Pill tone={s.tone}>{s.label}</Pill>;
}

/** The tool's brand mark where one ships, a lettermark otherwise. */
export function ToolGlyph({ provider, label, size = 7 }: { provider: string; label: string; size?: GlyphSize }) {
    return <ProviderGlyph provider={provider} name={label} size={size} />;
}
