import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteTeam from "@/lib/api/client/app/teams/deleteTeam";

export default function useDeleteTeam() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteTeam(id),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ["teams"] }),
    });
}
