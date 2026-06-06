import { useMutation } from "@tanstack/react-query";
import write from "@/lib/api/client/app/generation/write";
import type { WriteRequest } from "@/lib/api/models/app/generation/Write";

// On-demand AI draft generation. A mutation, not a query, because it's run
// explicitly from the "Write with AI" control. A 402 (out of credits) rejects
// with an AppError carrying status 402 for the call site to message.
export default function useGenerateWrite() {
    return useMutation({
        mutationFn: (body: WriteRequest) => write(body),
    });
}
