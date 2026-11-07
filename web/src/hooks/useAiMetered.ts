// useAiMetered: whether AI use is charged to the workspace balance here.
//
// False when billing is off (BILLING_PROVIDER=none, the self-host default): the
// backend bypasses the ledger, so balances and per-action prices would
// describe a meter that is not running. Surfaces drop that copy instead.

import useFeatureAccess from "@/hooks/useFeatureAccess";

export default function useAiMetered(): boolean {
    return useFeatureAccess().billing;
}
