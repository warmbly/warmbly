import { useQuery } from "@tanstack/react-query";
import getGoogleConnection from "@/lib/api/client/app/leadsync/getGoogleConnection";

// Reports whether the org has a connected google_sheets OAuth connection
// usable for lead-sync. Drives the "Connect Google Sheets" vs sheet-picker UI.
export default function useGoogleConnection() {
    return useQuery({
        queryKey: ["lead-sync", "google", "connection"],
        queryFn: getGoogleConnection,
        staleTime: 10_000,
    });
}
