import { useMutation } from "@tanstack/react-query";
import getSpreadsheet from "@/lib/api/client/app/leadsync/getSpreadsheet";
import previewSheet from "@/lib/api/client/app/leadsync/previewSheet";

// Fetching spreadsheet metadata and previewing a tab are user-triggered
// actions inside the wizard (not background queries), so they're mutations.
export function useGetSpreadsheet() {
    return useMutation({ mutationFn: getSpreadsheet });
}

export function usePreviewSheet() {
    return useMutation({ mutationFn: previewSheet });
}
