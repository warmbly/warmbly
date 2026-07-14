import { useQuery } from "@tanstack/react-query";
import { listAgentDrafts } from "@/lib/api/client/app/unibox/agentDrafts";

// useAgentDrafts fetches the org's pending inbox-agent drafts. The realtime
// AI_DRAFT_READY event invalidates ["unibox", "agent-drafts"] so the list stays
// live as the agent drafts on new replies.
export default function useAgentDrafts() {
    return useQuery({
        queryKey: ["unibox", "agent-drafts"],
        queryFn: listAgentDrafts,
        staleTime: 15_000,
        refetchOnWindowFocus: true,
    })
}
