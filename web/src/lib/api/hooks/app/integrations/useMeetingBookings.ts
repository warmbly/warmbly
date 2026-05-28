import { useQuery } from "@tanstack/react-query";
import listMeetingBookings from "@/lib/api/client/app/integrations/listMeetingBookings";

export default function useMeetingBookings() {
    return useQuery({
        queryKey: ["integrations", "bookings"],
        queryFn: listMeetingBookings,
        staleTime: 10_000,
    });
}
