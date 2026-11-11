// What each agency plan adds, by plan name; shared by the agency's billing page and the workspace Plans tab.
export const AGENCY_PLAN_FEATURES: Record<string, string[]> = {
    Agency: [
        "Unlimited clients, no per-client fee",
        "The API and webhooks in every client workspace",
        "Your logo, colour and portal domain on client pages",
        "Shared send pool and AI balance across clients",
    ],
    "Agency Pro": [
        "Everything in Agency",
        "No \"Powered by Warmbly\" on client pages",
        "Sending kept apart from other customers",
    ],
};
