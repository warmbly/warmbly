// Replaces the full conversation-label set on a thread (idempotent PUT).
// PUT /unibox/thread/labels { thread_id, category_ids }

import Request from "../../Request";
import type MiniCategory from "@/lib/api/models/app/contacts/MiniCategory";

interface Response {
  data: MiniCategory[];
}

export default async function setThreadLabels(
  threadId: string,
  categoryIds: string[],
): Promise<MiniCategory[]> {
  const res = await Request<Response>({
    method: "PUT",
    url: `/unibox/thread/labels`,
    data: { thread_id: threadId, category_ids: categoryIds },
    authorization: true,
  });
  return res.data ?? [];
}
