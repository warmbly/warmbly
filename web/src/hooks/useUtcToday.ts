import { useEffect, useState } from "react";
import { utcToday } from "@/lib/campaignPeriod";

// Today's UTC date, rolling over at midnight for a page left open.
export default function useUtcToday(): string {
    const [today, setToday] = useState(utcToday);
    useEffect(() => {
        const t = window.setInterval(() => setToday(utcToday()), 60_000);
        return () => window.clearInterval(t);
    }, []);
    return today;
}
