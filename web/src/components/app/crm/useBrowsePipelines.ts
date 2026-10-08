import { useContext } from "react";
import { useQuery } from "@tanstack/react-query";
import { UserContext } from "@/hooks/context/user";
import { useAppStore } from "@/stores/useAppStore";
import { pipelinesQuery } from "@/lib/api/hooks/app/crm/pipelines/usePipelines";

export default function useBrowsePipelines() {
    const userId = useContext(UserContext)?.user.id;
    const workspaceId = useAppStore((state) => state.currentOrganization?.id ?? "personal");
    // Pipeline fallbacks must never use the previous workspace's cached list.
    return useQuery({
        ...pipelinesQuery,
        queryKey: [...pipelinesQuery.queryKey, "browse", userId ?? "anonymous", workspaceId],
    });
}
