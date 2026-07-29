// Runtime configuration override.
//
// Vite bakes `import.meta.env.VITE_*` at build time, which would tie one built
// image to one deployment's URLs. To ship a single reusable image, the
// production container serves `/config.js` (generated from container env at
// startup) that sets `window.__WARMBLY_ENV__`. `runtimeEnv` prefers that value,
// then the value Vite baked at build time, then a fallback. In dev the
// `public/config.js` stub leaves the window value empty, so the baked value is
// used and behavior is unchanged.

type RuntimeEnv = Record<string, string | undefined>;

declare global {
    interface Window {
        __WARMBLY_ENV__?: RuntimeEnv;
    }
}

function fromWindow(key: string): string | undefined {
    if (typeof window === "undefined") return undefined;
    const value = window.__WARMBLY_ENV__?.[key];
    // Ignore empties and any unsubstituted "${VAR}" placeholder.
    if (!value || value.startsWith("${")) return undefined;
    return value;
}

// runtimeEnv returns the container-injected value, else the build-time value,
// else the fallback.
export function runtimeEnv(key: string, buildTime?: string, fallback = ""): string {
    return fromWindow(key) ?? (buildTime ? buildTime : undefined) ?? fallback;
}
