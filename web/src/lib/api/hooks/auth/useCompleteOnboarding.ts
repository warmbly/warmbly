import { useMutation } from "@tanstack/react-query";
import completeOnboarding from "../../client/auth/completeOnboarding";

interface CompleteOnboardingData {
    first_name: string;
    last_name: string;
    referral_source: string;
}

export default function useCompleteOnboarding() {
    return useMutation({
        mutationFn: (data: CompleteOnboardingData) => completeOnboarding(data),
    })
}
