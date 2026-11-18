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
    // Must be the preview rendered for contactId, never one still showing the previous lead.
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
    const keyRef = React.useRef(key);
    keyRef.current = key;
    React.useEffect(() => {
        setState(undefined);
        setFresh([]);
    }, [key]);

    const pending = preview?.lead ? (preview.ai_blocks ?? []).filter((b) => !b.written) : [];
    const blockIds = (preview?.ai_blocks ?? []).map((b) => b.id).join(",");
    const eligible = enabled && canWrite && !!preview?.lead && !preview.personalized;

    const write = React.useCallback(async () => {
        if (!campaignId || !stepId || !contactId || !eligible) return;
        const owner = key;
        setState("writing");
        try {
            await writeLeadAIOnce(qc, campaignId, contactId, stepId);
            // The write still lands in the cache; only the lead it was for shows it.
            if (keyRef.current !== owner) return;
            setFresh(blockIds ? blockIds.split(",") : []);
            setState(undefined);
            onWritten();
        } catch (err) {
            if (keyRef.current !== owner) return;
            const code = (err as AppError).code;
            setState(code === "insufficient_credits" ? "balance" : code === "usage_cap_exceeded" ? "cap" : code === "ai_unavailable" ? "unavailable" : "error");
        }
    }, [qc, campaignId, stepId, contactId, key, eligible, blockIds, onWritten]);

    const pendingKey = pending.map((b) => b.id).join(",");
    React.useEffect(() => {
        if (!eligible || !key || !pendingKey) return;
        // Once per lead and step, so a block the write cannot fill is not retried in a loop.
        if (attempted.current.has(key)) return;
        attempted.current.add(key);
        void write();
    }, [eligible, key, pendingKey, write]);

    return { state, fresh, pending, canWrite, write };
}
