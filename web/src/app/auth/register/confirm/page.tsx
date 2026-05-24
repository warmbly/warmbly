import { Link } from "react-router-dom";
import { TurnstileModal } from "@/components/captcha/TurnstileModal";
import AuthButton from "@/components/auth/button";
import OTPInput from "@/components/auth/OTP";
import { useRegisterConfirmForm } from "../../hooks/useRegisterConfirmForm";
import { ArrowLeft } from "lucide-react";

export default function RegisterConfirmPage() {
    const { mail, otp, setOtp, captcha, pending, onSubmit, onToken } = useRegisterConfirmForm();

    return (
        <div className="space-y-6">
            <div className="text-center">
                <div className="mx-auto w-14 h-14 rounded-2xl bg-sky-50 flex items-center justify-center mb-4">
                    <svg className="w-7 h-7 text-sky-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M21.75 6.75v10.5a2.25 2.25 0 01-2.25 2.25h-15a2.25 2.25 0 01-2.25-2.25V6.75m19.5 0A2.25 2.25 0 0019.5 4.5h-15a2.25 2.25 0 00-2.25 2.25m19.5 0v.243a2.25 2.25 0 01-1.07 1.916l-7.5 4.615a2.25 2.25 0 01-2.36 0L3.32 8.91a2.25 2.25 0 01-1.07-1.916V6.75" />
                    </svg>
                </div>
                <h1 className="font-serif text-[32px] text-slate-800 tracking-tight leading-tight">Almost there</h1>
                <p className="text-sm text-slate-400 mt-1.5">
                    Enter the code we sent{mail ? " to " : ""}
                    {mail && <span className="text-slate-600 font-medium">{mail}</span>}
                </p>
            </div>

            <form onSubmit={onSubmit} className="space-y-5">
                <OTPInput value={otp} setValue={setOtp} />
                <AuthButton loading={pending}>Verify</AuthButton>
                <TurnstileModal visible={captcha} onToken={onToken} />
            </form>

            <div className="text-center pt-1">
                <Link to="/auth/register" className="inline-flex items-center gap-1 text-sm text-slate-400 hover:text-slate-600 transition-colors">
                    <ArrowLeft className="w-4 h-4" />
                    Back to sign up
                </Link>
            </div>
        </div>
    );
}
