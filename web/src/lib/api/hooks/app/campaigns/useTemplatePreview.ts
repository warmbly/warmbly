import { useMutation } from "@tanstack/react-query";
import { previewTemplate } from "@/lib/api/client/app/campaigns/previewTemplate";

export function useTemplatePreview() {
    return useMutation({ mutationFn: previewTemplate });
}
