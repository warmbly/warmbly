import type Subscription from "@/lib/api/models/app/subscription/Subscription";
import type { AppError } from "../../normalizeError";
import Request from "../../Request";

// null is a workspace with no subscription yet (one made after its owner's first trial), which checkout creates.
export default async function getSubscription(): Promise<Subscription | null> {
    try {
        return await Request<Subscription>({
            method: "GET",
            url: `/subscription`,
            authorization: true,
        });
    } catch (error) {
        if ((error as AppError | null)?.status === 404) return null;
        throw error;
    }
}
