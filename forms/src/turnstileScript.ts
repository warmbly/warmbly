// Cloudflare Turnstile script plumbing, split from the widget component so
// the component file only exports components (react-refresh constraint).

export interface TurnstileAPI {
    render: (el: HTMLElement, opts: Record<string, unknown>) => string;
    remove: (id: string) => void;
    reset: (id?: string) => void;
}

declare global {
    interface Window {
        turnstile?: TurnstileAPI;
    }
}

const SCRIPT_SRC = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";

let scriptLoading: Promise<void> | null = null;

export function loadTurnstileScript(): Promise<void> {
    if (window.turnstile) return Promise.resolve();
    scriptLoading ??= new Promise((resolve, reject) => {
        const s = document.createElement("script");
        s.src = SCRIPT_SRC;
        s.async = true;
        s.onload = () => resolve();
        s.onerror = () => reject(new Error("turnstile script failed to load"));
        document.head.appendChild(s);
    });
    return scriptLoading;
}

export function resetTurnstile() {
    try {
        window.turnstile?.reset();
    } catch {
        // a widget that was never rendered has nothing to reset
    }
}
