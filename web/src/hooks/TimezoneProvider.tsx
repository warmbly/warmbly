"use client";

import { API_BASE_URL } from "@/lib/information";
import React, { createContext, useContext } from "react";
import { useError } from "./ErrorProvider";

interface TimezoneOption {
    name: string;
    display_name: string;
}

export const TimezoneContext = createContext<TimezoneOption[] | null>(null);

export default function TimezoneProvider({children}:{children: React.ReactNode}){
    const { showError } = useError();
    const [timezones, setTimezones] = React.useState<TimezoneOption[] | null>(null);

    React.useEffect(() => {
        const Do = async() => {
            try {
                const resp = await fetch(`${API_BASE_URL}/timezones`)
                if (!resp.ok){
                    showError(`Error ${resp.status}`, "Something went wrong when fetching the timezones.")
                } else {
                    const newTzs: TimezoneOption[] = await resp.json()
                    setTimezones(newTzs)
                }
            } catch (err) {
                showError("Client Error", `${err}`)
            }
        }
        Do()
    }, [])

    return <>
        <TimezoneContext.Provider value={timezones}>
            {children}
        </TimezoneContext.Provider>
    </>
}

export function useTimezones() {
    return useContext(TimezoneContext)
}