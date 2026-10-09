import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getSuggestedSenders, setMemberAccess } from "@/lib/api/client/app/organizations/memberAccess";
import type MemberAccess from "@/lib/api/models/app/organizations/MemberAccess";
import type OrganizationMember from "@/lib/api/models/app/organizations/OrganizationMember";

export function useSetMemberAccess() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ userId, access }: { userId: string; access: MemberAccess }) => setMemberAccess(userId, access),
        onMutate: async ({ userId, access }) => {
            await queryClient.cancelQueries({ queryKey: ["organizations", "members"] });
            const previous = queryClient.getQueryData<OrganizationMember[]>(["organizations", "members"]);
            queryClient.setQueryData<OrganizationMember[]>(["organizations", "members"], (list) =>
                list?.map((m) => (m.user_id === userId ? { ...m, access_scope: access.scope, access } : m)),
            );
            return { previous };
        },
        onError: (_err, _vars, ctx) => {
            if (ctx?.previous) queryClient.setQueryData(["organizations", "members"], ctx.previous);
        },
        onSettled: () => {
            queryClient.invalidateQueries({ queryKey: ["organizations", "members"] });
        },
    });
}

export function useSuggestedSenders(campaignIds: string[], folderIds: string[], enabled: boolean) {
    return useQuery({
        queryKey: ["organizations", "access", "suggested-senders", [...campaignIds].sort(), [...folderIds].sort()],
        queryFn: () => getSuggestedSenders(campaignIds, folderIds),
        enabled: enabled && campaignIds.length + folderIds.length > 0,
        staleTime: 30_000,
    });
}
