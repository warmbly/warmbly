// Sky chrome — the calm light backdrop behind sidebar + header.
//
// Intent: an off-white workspace, just slightly cooler than the white
// content panel, with the faintest suggestion of clouds drifting in
// the upper area. The atmosphere is meant to be felt, not seen — if
// you find yourself noticing it, it's too loud.
//
// What's painted:
//   - A single near-white base (#f4f7fb), one degree cooler than the
//     white content surface so the panel reads slightly forward.
//   - Three blurred-white cloud blobs in the top half, very low
//     opacity. No animation; the auth page is the loud version.
//   - A subtle vignette toward the bottom so the chrome doesn't feel
//     flat across large monitors.
//
// Pointer-events-none across every layer so nothing intercepts input.

import React from "react";

export function SkyChrome() {
    return (
        <div className="absolute inset-0 overflow-hidden pointer-events-none">
            {/* Base tint — clean neutral, a half-step darker than white.
                Avoid blue-leaning here; the only colour cue is the soft
                clouds above. */}
            <div className="absolute inset-0 bg-[#f5f6f8]" />

            {/* Faint cloud blobs. Positioned to suggest a softly clouded
                sky in the upper half. Generous blur radius + low alpha
                makes them ambient rather than figurative. */}
            <div
                className="absolute"
                style={{
                    top: "-80px",
                    left: "-60px",
                    width: "520px",
                    height: "240px",
                    background: "rgba(255,255,255,0.95)",
                    borderRadius: "50% 50% 18% 18%",
                    filter: "blur(60px)",
                    opacity: 0.7,
                }}
            />
            <div
                className="absolute"
                style={{
                    top: "10%",
                    right: "8%",
                    width: "380px",
                    height: "180px",
                    background: "rgba(255,255,255,0.85)",
                    borderRadius: "50% 50% 18% 18%",
                    filter: "blur(70px)",
                    opacity: 0.55,
                }}
            />
            <div
                className="absolute"
                style={{
                    top: "32%",
                    left: "26%",
                    width: "300px",
                    height: "140px",
                    background: "rgba(255,255,255,0.9)",
                    borderRadius: "50% 50% 22% 22%",
                    filter: "blur(70px)",
                    opacity: 0.45,
                }}
            />

        </div>
    );
}
