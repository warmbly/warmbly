// The frame of an agency page: its name in the topbar beside the agency's.

import React from "react";
import { Page, PageTopbar } from "@/components/layout/Page";
import { useCurrentAgency } from "@/lib/api/hooks/app/agency/useAgency";

export function AgencyPage({ title, actions, children }: { title: string; actions?: React.ReactNode; children: React.ReactNode }) {
    const { agency } = useCurrentAgency();
    return (
        <Page>
            <PageTopbar eyebrow={title} subtitle={agency?.name}>
                {actions}
            </PageTopbar>
            {children}
        </Page>
    );
}
