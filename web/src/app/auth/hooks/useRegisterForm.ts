import React, { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTurnstile } from "react-turnstile";
import useRegister from "@/lib/api/hooks/auth/useRegister";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useRegisterForm() {
    const navigate = useNavigate();
    const turnstile = useTurnstile();
    const register = useRegister();

    const [mail, setMail] = useState("");
    const [password, setPassword] = useState("");
    const [password2, setPassword2] = useState("");
    const [acceptTerms, setAcceptTerms] = useState(false);
    const [tk, setTk] = useState("");
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const reset = () => { setTk(""); turnstile.reset(); };

    const submit = async () => {
        if (pending) return;
        if (!acceptTerms) { toast.error("Please accept the Terms of Service and Privacy Policy to continue."); return; }
        if (tk === "") { setCaptcha(true); return; }
        if (password !== password2) { toast.error("Passwords don't match. Please make sure you type the same password twice."); return; }
        setPending(true);
        try {
            const r = await toast.promise(
                register.mutateAsync({ email: mail, password, turnstile: tk }),
                { loading: "Loading...", error: (err: AppError) => buildError(err) }
            );
            navigate(`/auth/register/confirm?session=${r.session}&to=${mail}`);
        } finally { setPending(false); reset(); }
    };

    const onSubmit = async (e: React.FormEvent) => { e.preventDefault(); await submit(); };
    const onToken = (t: string) => { setTk(t); if (captcha) { setCaptcha(false); if (tk) submit(); } };

    return { mail, setMail, password, setPassword, password2, setPassword2, acceptTerms, setAcceptTerms, captcha, pending, onSubmit, onToken };
}
