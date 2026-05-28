import type { MeetingBooking } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function listMeetingBookings(): Promise<{ bookings: MeetingBooking[] }> {
    return await Request<{ bookings: MeetingBooking[] }>({
        method: "GET",
        url: "/integrations/bookings",
        authorization: true,
    });
}
