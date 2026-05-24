import type Timezone from "../../models/app/Timezone";
import Request from "../Request";

export default async function getTimezones(): Promise<Timezone[]> {
    return await Request<Timezone[]>({
        method: "GET",
        url: "/timezones",
        authorization: true,
    })
}
