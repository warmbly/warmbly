// Shared page header. Every page slots its title + tagline + optional
// actions in here so the spacing/typography stays consistent.

import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface PageHeaderProps {
    title: string;
    description?: string;
    children?: ReactNode;
    className?: string;
}

export function PageHeader({ title, description, children, className }: PageHeaderProps) {
    return (
        <div
            className={cn(
                "mb-6 flex flex-col gap-2 md:flex-row md:items-end md:justify-between",
                className,
            )}
        >
            <div className="min-w-0">
                <h1 className="text-2xl font-semibold tracking-tight text-foreground">
                    {title}
                </h1>
                {description && (
                    <p className="text-sm text-muted-foreground mt-1 max-w-2xl">
                        {description}
                    </p>
                )}
            </div>
            {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
        </div>
    );
}

// Used by stub pages so the "coming soon" treatment is visually
// consistent across the app instead of every page reinventing it.
export function ComingSoon({ label }: { label: string }) {
    return (
        <div className="rounded-lg border border-dashed border-border p-10 text-center">
            <div className="text-sm font-medium text-foreground">{label}</div>
            <div className="text-xs text-muted-foreground mt-1">
                This surface is part of the admin app, but the page implementation
                will land in a follow-up iteration.
            </div>
        </div>
    );
}
