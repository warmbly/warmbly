// useAiMetered — whether AI use is charged in credits on this deployment.
//
// False when billing is off (BILLING_PROVIDER=none, the self-host default): the
// backend bypasses the credit ledger, so balances, allowances and per-action
// "costs N credits" copy would describe a meter that is not running. Surfaces
// use this to drop that copy rather than show a plan-based figure.

import useFeatureAccess from "@/hooks/useFeatureAccess";

export default function useAiMetered(): boolean {
    return useFeatureAccess().billing;
}
