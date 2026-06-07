import { useMutation, useQueryClient } from "@tanstack/react-query";
import addTeamMember from "@/lib/api/client/app/teams/addTeamMember";

export default function useAddTeamMember() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, userId }: { id: string; userId: string }) => addTeamMember(id, userId),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ["teams"] }),
    });
}
