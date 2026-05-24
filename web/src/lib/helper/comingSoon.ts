// Tiny helper for "Coming soon" toasts on actions whose backend or
// flow isn't built yet. Better than a silent no-op — the user sees the
// click registered and gets a clear "this isn't wired yet" signal
// instead of wondering if the page is broken.

import toast from "react-hot-toast";

export function comingSoon(feature: string): void {
    toast(`${feature} is coming soon.`, {
        icon: "🚧",
        duration: 3000,
    });
}
