// The handful of line icons the client pages draw. They take the text colour and are hidden from screen readers.

import type { ReactNode } from "react";

function Icon({ children, className }: { children: ReactNode; className?: string }) {
    return (
        <svg
            className={className ? `pt-icon ${className}` : "pt-icon"}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
        >
            {children}
        </svg>
    );
}

export function CheckIcon() {
    return (
        <Icon>
            <circle cx="12" cy="12" r="9" />
            <path d="m8.4 12.3 2.5 2.5 4.8-5.2" />
        </Icon>
    );
}

export function AlertIcon() {
    return (
        <Icon>
            <circle cx="12" cy="12" r="9" />
            <path d="M12 7.8v4.9" />
            <path d="M12 16.1h.01" />
        </Icon>
    );
}

export function QuestionIcon() {
    return (
        <Icon>
            <circle cx="12" cy="12" r="9" />
            <path d="M9.6 9.5a2.5 2.5 0 0 1 4.8.9c0 1.7-2.4 2-2.4 3.5" />
            <path d="M12 16.7h.01" />
        </Icon>
    );
}

export function ClockIcon() {
    return (
        <Icon>
            <circle cx="12" cy="12" r="9" />
            <path d="M12 7.5V12l3 1.8" />
        </Icon>
    );
}

export function MailIcon({ className }: { className?: string }) {
    return (
        <Icon className={className}>
            <rect x="3.5" y="5.5" width="17" height="13" rx="2" />
            <path d="m4.2 7.6 7.8 5.4 7.8-5.4" />
        </Icon>
    );
}

export function BackIcon() {
    return (
        <Icon>
            <path d="M19 12H5" />
            <path d="m11 6-6 6 6 6" />
        </Icon>
    );
}

export function PrintIcon() {
    return (
        <Icon>
            <path d="M7 8.5V4h10v4.5" />
            <rect x="3.5" y="8.5" width="17" height="8" rx="1.5" />
            <path d="M7 13.5h10V20H7z" />
        </Icon>
    );
}

export function ChevronIcon({ className }: { className?: string }) {
    return (
        <Icon className={className}>
            <path d="m6 9 6 6 6-6" />
        </Icon>
    );
}

export function StackIcon({ className }: { className?: string }) {
    return (
        <Icon className={className}>
            <path d="m12 3.5 8.5 4.3L12 12 3.5 7.8 12 3.5Z" />
            <path d="m3.5 12 8.5 4.2 8.5-4.2" />
            <path d="m3.5 16.2 8.5 4.3 8.5-4.3" />
        </Icon>
    );
}
