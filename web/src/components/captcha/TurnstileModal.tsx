import Turnstile from './Turnstile';

interface Props {
    visible: boolean;
    onToken: (t: string) => void;
}

export function TurnstileModal({ visible, onToken }: Props) {
    return (
        <div className={`fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 backdrop-blur-sm transition-all duration-300 ${visible ? "opacity-100 visible" : "opacity-0 invisible pointer-events-none"}`}>
            <div className="rounded-2xl bg-white p-8 shadow-[0_25px_50px_-12px_rgba(0,0,0,0.25)]">
                <Turnstile setToken={onToken} />
            </div>
        </div>
    );
}
