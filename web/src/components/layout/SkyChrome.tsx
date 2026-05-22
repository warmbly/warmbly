// Sky chrome — the background that lives behind the sidebar + header.
//
// Goal: when you look at the app you feel like the work surface (the
// content panel) is a clean room with sky outside the window frames.
// The auth page is INSIDE the sky; the dashboard chrome is a quieter
// echo of that — same palette, less drama, no animated clouds racing
// across the screen.
//
// Visually:
//   - Soft gradient from a deeper sky at the top of the sidebar down
//     to a brighter wash near the content corner
//   - Two very faint blurred glows for atmosphere; no animation, no
//     hard cloud shapes. The dashboard should feel quiet.
//
// The component renders nothing structurally — it's purely decorative.
// Position absolute / pointer-events none so it never intercepts clicks.

import React from "react";

export function SkyChrome() {
    return (
        <div className="absolute inset-0 overflow-hidden pointer-events-none">
            {/* Base gradient — top-left rich sky, fades brighter toward the
                content panel corner so the eye is drawn to the work. */}
            <div
                className="absolute inset-0"
                style={{
                    background:
                        "linear-gradient(155deg, #0c4a6e 0%, #075985 18%, #0369a1 40%, #0284c7 65%, #38bdf8 100%)",
                }}
            />

            {/* Warm bloom near the top-left — like indirect sunlight catching
                the wordmark. Subtle, doesn't dominate. */}
            <div
                className="absolute -top-32 -left-32 w-[640px] h-[640px] rounded-full blur-3xl opacity-50"
                style={{
                    background:
                        "radial-gradient(circle, rgba(186,230,253,0.35) 0%, rgba(125,211,252,0.15) 35%, transparent 70%)",
                }}
            />

            {/* Cool wash near the bottom-right of the sidebar — gives the
                gradient depth without an obvious second color. */}
            <div
                className="absolute -bottom-40 -right-20 w-[520px] h-[520px] rounded-full blur-3xl opacity-40"
                style={{
                    background:
                        "radial-gradient(circle, rgba(56,189,248,0.25) 0%, rgba(14,165,233,0.10) 40%, transparent 75%)",
                }}
            />

            {/* Very faint noise / cloud suggestion — a single soft blob
                placed off-axis. Don't add more; the auth page is the place
                for theatrical clouds, this is the quiet cousin. */}
            <div
                className="absolute top-[35%] left-[20%] w-[280px] h-[120px] rounded-full blur-2xl opacity-25"
                style={{
                    background: "rgba(255,255,255,0.6)",
                }}
            />
        </div>
    );
}
