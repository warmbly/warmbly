import type { CreateMeetingInput, MeetingBooking } from "@/lib/api/models/app/integrations/Integration";
import Request from "../../Request";

export default async function createMeeting(input: CreateMeetingInput): Promise<{ meeting: MeetingBooking }> {
    return await Request<{ meeting: MeetingBooking }>({
        method: "POST",
        url: "/meetings",
        data: input,
        authorization: true,
    });
}
