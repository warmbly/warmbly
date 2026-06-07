import { useMutation, useQueryClient } from "@tanstack/react-query";
import updateTeam from "@/lib/api/client/app/teams/updateTeam";

export default function useUpdateTeam() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: { name?: string; color?: string } }) =>
            updateTeam(id, data),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ["teams"] }),
    });
}
