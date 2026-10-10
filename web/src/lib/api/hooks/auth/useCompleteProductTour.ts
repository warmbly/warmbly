import { useMutation, useQueryClient } from "@tanstack/react-query";
import completeProductTour from "../../client/auth/completeProductTour";
import type User from "@/lib/api/models/auth/User";

// Marks the tour done on the cached /auth/me user before the request, so it
// cannot open again while the write is in flight. A failed write only means
// it is offered once more on the next visit, so nothing is rolled back.
export default function useCompleteProductTour() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: () => completeProductTour(),
        onMutate: () => {
            qc.setQueryData<User | null>(["auth", "me"], (old) =>
                old && !old.product_tour_completed_at ? { ...old, product_tour_completed_at: new Date() } : old,
            );
        },
    });
}
