import { useEffect } from "react";
import * as TurnstileObj from "react-turnstile";
import { TURNSTILE_KEY } from "@/lib/information";

interface Props {
    setToken: (token: string) => void;
}

export default function Turnstile({setToken}: Props) {
    const defaultDevBypassToken = "warmbly-local-turnstile-bypass";
    const bypassToken = import.meta.env.DEV
        ? (import.meta.env.VITE_TURNSTILE_BYPASS_TOKEN?.trim() || defaultDevBypassToken)
        : "";

    useEffect(() => {
        if (bypassToken) setToken(bypassToken);
    }, [bypassToken, setToken]);

    if (bypassToken) return null;

    return <>
        <TurnstileObj.default
            sitekey={TURNSTILE_KEY}
            onVerify={(token: string) => setToken(token)}
            onExpire={() => setToken("")}
            theme={"light"}
        />
    </>
}
