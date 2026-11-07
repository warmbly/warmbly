import toast from "react-hot-toast/headless";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { isInsufficientBalance, openInsufficientBalance } from "@/lib/insufficientBalance";

// reportAIError reports a failed AI call. A balance that ran out opens the
// add-money dialog instead of a toast.
export function reportAIError(e: unknown) {
    if (isInsufficientBalance(e)) {
        openInsufficientBalance();
        return;
    }
    toast.error(buildError(e as AppError));
}
