import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteMeeting from "@/lib/api/client/app/meetings/deleteMeeting";

export default function useDeleteMeeting() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: deleteMeeting,
        onSuccess: () => {
            void qc.invalidateQueries({ queryKey: ["meetings"] });
            void qc.invalidateQueries({ queryKey: ["meetings", "summary"] });
        },
    });
}
