import React from "react";
import { useQueryClient } from "@tanstack/react-query";
import { writeLeadAIOnce } from "@/lib/api/hooks/app/campaigns/useCampaignReview";
import type { TemplatePreview } from "@/lib/api/client/app/campaigns/previewTemplate";
import type { AppError } from "@/lib/api/client/normalizeError";
import { usePermission } from "@/hooks/usePermission";

export type PreviewAIState = "writing" | "balance" | "cap" | "unavailable" | "error";

// Writes the previewed lead's AI copy for a saved step as the preview opens on
// them, as the Review tab does, so the preview shows what they will get.
export function usePreviewLeadAI({
    campaignId,
    stepId,
    contactId,
    preview,
    enabled,
    onWritten,
}: {
    campaignId?: string;
    stepId?: string;
    contactId?: string;
    preview: TemplatePreview | null;
    // Off while the step has unsaved edits: the write reads the saved step.
    enabled: boolean;
    onWritten: () => void;
}) {
    const qc = useQueryClient();
    const canAI = usePermission("USE_AI");
    const canEdit = usePermission("MANAGE_CAMPAIGNS");
    const canWrite = canAI && canEdit;
    const [state, setState] = React.useState<PreviewAIState | undefined>();
    const [fresh, setFresh] = React.useState<string[]>([]);
    const attempted = React.useRef(new Set<string>());

    const key = campaignId && stepId && contactId ? `${contactId}:${stepId}` : "";
    React.useEffect(() => {
        setState(undefined);
        setFresh([]);
    }, [key]);

    const pending = preview?.lead ? (preview.ai_blocks ?? []).filter((b) => !b.written) : [];
    const blockIds = (preview?.ai_blocks ?? []).map((b) => b.id).join(",");

    const write = React.useCallback(async () => {
        if (!campaignId || !stepId || !contactId) return;
        setState("writing");
        try {
            await writeLeadAIOnce(qc, campaignId, contactId, stepId);
            setFresh(blockIds ? blockIds.split(",") : []);
            setState(undefined);
            onWritten();
        } catch (err) {
            const code = (err as AppError).code;
            setState(code === "insufficient_credits" ? "balance" : code === "usage_cap_exceeded" ? "cap" : code === "ai_unavailable" ? "unavailable" : "error");
        }
    }, [qc, campaignId, stepId, contactId, blockIds, onWritten]);

    const pendingKey = pending.map((b) => b.id).join(",");
    React.useEffect(() => {
        if (!enabled || !key || !pendingKey || !canWrite || preview?.personalized) return;
        // Once per lead and step, so a block the write cannot fill is not retried in a loop.
        if (attempted.current.has(key)) return;
        attempted.current.add(key);
        void write();
    }, [enabled, key, pendingKey, canWrite, preview?.personalized, write]);

    return { state, fresh, pending, canWrite, write };
}
