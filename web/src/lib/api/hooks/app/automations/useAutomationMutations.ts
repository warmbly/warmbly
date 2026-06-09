import { useMutation, useQueryClient } from "@tanstack/react-query";
import createAutomation from "@/lib/api/client/app/automations/createAutomation";
import updateAutomation from "@/lib/api/client/app/automations/updateAutomation";
import deleteAutomation from "@/lib/api/client/app/automations/deleteAutomation";
import testAutomation from "@/lib/api/client/app/automations/testAutomation";
import type { AutomationWrite } from "@/lib/api/models/app/automations/Automation";

export function useTestAutomation() {
    return useMutation({
        mutationFn: ({ id, data }: { id: string; data?: Record<string, unknown> }) => testAutomation(id, data),
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

export function useDeleteAutomation() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteAutomation(id),
        onSuccess: () => void qc.invalidateQueries({ queryKey: ["automations"] }),
    });
}
