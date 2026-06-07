import { useMutation, useQueryClient } from "@tanstack/react-query";
import removeTeamMember from "@/lib/api/client/app/teams/removeTeamMember";

export default function useRemoveTeamMember() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, userId }: { id: string; userId: string }) => removeTeamMember(id, userId),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ["teams"] }),
    });
}
