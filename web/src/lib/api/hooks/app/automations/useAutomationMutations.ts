import { useMutation, useQueryClient } from "@tanstack/react-query";
import createAutomation from "@/lib/api/client/app/automations/createAutomation";
import updateAutomation from "@/lib/api/client/app/automations/updateAutomation";
import updateAutomationLayout, { type NodePosition } from "@/lib/api/client/app/automations/updateAutomationLayout";
import deleteAutomation from "@/lib/api/client/app/automations/deleteAutomation";
import testAutomation from "@/lib/api/client/app/automations/testAutomation";
import type { AutomationWrite } from "@/lib/api/models/app/automations/Automation";

export function useTestAutomation() {
    return useMutation({
        mutationFn: ({ id, data, skipNodeIds }: { id: string; data?: Record<string, unknown>; skipNodeIds?: string[] }) =>
            testAutomation(id, data, skipNodeIds),
    });
}

export function useCreateAutomation() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (w: AutomationWrite) => createAutomation(w),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["automations"] }),
    });
}

export function useUpdateAutomation() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: ({ id, w }: { id: string; w: AutomationWrite }) => updateAutomation(id, w),
        onSuccess: (_res, vars) => {
            void qc.invalidateQueries({ queryKey: ["automations"] });
            void qc.invalidateQueries({ queryKey: ["automations", vars.id] });
        },
    });
}

// Persist node coordinates only. Deliberately does NOT invalidate the automation
// query: positions are already on the open canvas, and a refetch would reseed it
// mid-edit. The server write is silent (no audit, no updated_at bump), so other
// teammates' editors are not disturbed either.
export function useUpdateAutomationLayout() {
    return useMutation({
        mutationFn: ({ id, positions }: { id: string; positions: NodePosition[] }) => updateAutomationLayout(id, positions),
    });
}

export function useDeleteAutomation() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteAutomation(id),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["automations"] }),
    });
}
