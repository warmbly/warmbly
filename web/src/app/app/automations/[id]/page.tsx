// Automation builder — the visual flow canvas for one automation.

import React from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Loader2Icon } from "lucide-react";
import { useAutomation } from "@/lib/api/hooks/app/automations/useAutomation";
import useIntegrationConnections from "@/lib/api/hooks/app/integrations/useIntegrationConnections";
import useIntegrationCatalog from "@/lib/api/hooks/app/integrations/useIntegrationCatalog";
import { EmptyBlock } from "@/components/layout/Page";
import AutomationFlow from "@/components/app/automations/AutomationFlow";

export default function AutomationBuilderPage() {
    const { id } = useParams();
    const navigate = useNavigate();
    const autoQ = useAutomation(id ?? "");
    const connQ = useIntegrationConnections();
    const catQ = useIntegrationCatalog();

    const back = () => navigate("/app/automations");

    // Wait for connections + catalog too: the canvas seeds action-node labels
    // (integration name, glyph, available actions) from them once on mount.
    if (autoQ.isLoading || connQ.isLoading || catQ.isLoading) {
        return (
            <div className="h-full flex items-center justify-center">
                <Loader2Icon className="w-5 h-5 text-slate-300 animate-spin" />
            </div>
        );
    }
    if (!autoQ.data) {
        return (
            <div className="h-full flex items-center justify-center">
                <EmptyBlock title="Automation not found" body="It may have been deleted." />
            </div>
        );
    }

    return (
        <AutomationFlow
            automation={autoQ.data.automation}
            connections={connQ.data?.connections ?? []}
            catalog={catQ.data?.catalog ?? []}
            onBack={back}
        />
    );
}
