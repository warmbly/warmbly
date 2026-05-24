import Request from "../Request";

interface CompleteOnboardingData {
    first_name: string;
    last_name: string;
    referral_source: string;
}

export default async function completeOnboarding(data: CompleteOnboardingData): Promise<void> {
    await Request<void>({
        method: "PATCH",
        url: "/auth/me/onboarding",
        data,
        authorization: true,
    })
}
