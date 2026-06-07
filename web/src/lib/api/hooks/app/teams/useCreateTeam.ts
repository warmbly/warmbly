import { useMutation, useQueryClient } from "@tanstack/react-query";
import createTeam from "@/lib/api/client/app/teams/createTeam";

export default function useCreateTeam() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: { name: string; color?: string }) => createTeam(data),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ["teams"] }),
    });
}
