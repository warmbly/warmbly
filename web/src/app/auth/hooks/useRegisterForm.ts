import type React from "react";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import useRegister from "@/lib/api/hooks/auth/useRegister";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

const REFERRAL_STORAGE_KEY = "warmbly_referral_code";

export function useRegisterForm() {
    const navigate = useNavigate();
    const register = useRegister();

    const [mail, setMail] = useState("");
    const [password, setPassword] = useState("");
    const [password2, setPassword2] = useState("");
    const [acceptTerms, setAcceptTerms] = useState(false);
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);
    const [referralCode, setReferralCode] = useState<string | null>(null);

    // Capture ?ref= on mount and persist it so it survives the email-code step
    // (and a page reload) until the account is actually created.
    useEffect(() => {
        const fromUrl = new URLSearchParams(window.location.search).get("ref")?.trim();
        if (fromUrl) {
            sessionStorage.setItem(REFERRAL_STORAGE_KEY, fromUrl);
            setReferralCode(fromUrl);
            return;
        }
        const stored = sessionStorage.getItem(REFERRAL_STORAGE_KEY);
        if (stored) setReferralCode(stored);
    }, []);

    const submit = async (token: string) => {
        setPending(true);
        try {
            const r = await toast.promise(
                register.mutateAsync({
                    email: mail,
                    password,
                    turnstile: token,
                    referral_code: referralCode ?? undefined,
                }),
                { loading: "Loading...", error: (err: AppError) => buildError(err) }
            );
            navigate(`/auth/register/confirm?session=${r.session}&to=${mail}`);
        } finally { setPending(false); }
    };

    const onSubmit = async (e: React.FormEvent) => {
        e.preventDefault();
        if (pending) return;
        if (!acceptTerms) { toast.error("Please accept the Terms of Service and Privacy Policy to continue."); return; }
        if (password !== password2) { toast.error("Passwords don't match. Please make sure you type the same password twice."); return; }
        setCaptcha(true);
    };
    const onToken = async (t: string) => { setCaptcha(false); await submit(t); };

    return { mail, setMail, password, setPassword, password2, setPassword2, acceptTerms, setAcceptTerms, captcha, pending, onSubmit, onToken, referralCode };
}
