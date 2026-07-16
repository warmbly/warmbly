// The assistant's logomark: a single geometric spark, monochrome by design.
// Rendered inside a plain slate-900 tile (see AgentPanel / AppHeader) instead
// of a pastel gradient box, so the assistant reads as part of the product
// chrome rather than a bolted-on AI widget.
export default function AgentMark({ className }: { className?: string }) {
    return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden className={className}>
            <path d="M12 0c.83 6.26 5.74 11.17 12 12-6.26.83-11.17 5.74-12 12-.83-6.26-5.74-11.17-12-12C6.26 11.17 11.17 6.26 12 0Z" />
        </svg>
    );
}
