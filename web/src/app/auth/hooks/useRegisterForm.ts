import type React from "react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import useRegister from "@/lib/api/hooks/auth/useRegister";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export function useRegisterForm() {
    const navigate = useNavigate();
    const register = useRegister();

    const [mail, setMail] = useState("");
    const [password, setPassword] = useState("");
    const [password2, setPassword2] = useState("");
    const [acceptTerms, setAcceptTerms] = useState(false);
    const [captcha, setCaptcha] = useState(false);
    const [pending, setPending] = useState(false);

    const submit = async (token: string) => {
        setPending(true);
        try {
            const r = await toast.promise(
                register.mutateAsync({ email: mail, password, turnstile: token }),
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

    return { mail, setMail, password, setPassword, password2, setPassword2, acceptTerms, setAcceptTerms, captcha, pending, onSubmit, onToken };
}
