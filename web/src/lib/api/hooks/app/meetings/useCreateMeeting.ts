import { useMutation, useQueryClient } from "@tanstack/react-query";
import createMeeting from "@/lib/api/client/app/meetings/createMeeting";

export default function useCreateMeeting() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: createMeeting,
        onSuccess: (res) => {
            void qc.invalidateQueries({ queryKey: ["meetings"] });
            void qc.invalidateQueries({ queryKey: ["meetings", "summary"] });
            const contactId = res.meeting.contact_id;
            if (contactId) {
                void qc.invalidateQueries({ queryKey: ["contacts", contactId, "timeline"] });
            }
        },
    });
}
