// The assistant's logomark: a large spark with a small companion spark, the
// glyph people read as "AI". Filled and geometric (crisper than the outlined
// lucide Sparkles), single-color so it takes whatever text color it is given
// and reads as product chrome rather than a bolted-on AI widget.
export default function AgentMark({ className }: { className?: string }) {
    return (
        <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden className={className}>
            <path d="M9.5 4c.62 4.7 3.3 7.38 8 8-4.7.62-7.38 3.3-8 8-.62-4.7-3.3-7.38-8-8 4.7-.62 7.38-3.3 8-8Z" />
            <path d="M18.5 2c.31 2.35 1.65 3.69 4 4-2.35.31-3.69 1.65-4 4-.31-2.35-1.65-3.69-4-4 2.35-.31 3.69-1.65 4-4Z" />
        </svg>
    );
}
