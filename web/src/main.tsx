import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './global.css'
import "@fontsource-variable/inter/opsz.css";

import { QueryClientProvider } from "@tanstack/react-query"
import { ReactQueryDevtools } from "@tanstack/react-query-devtools"
import { RouterProvider } from "@tanstack/react-router";

import { Toaster } from '@/components/ui/toaster';
import { initErrorReporting } from "@/lib/observability";
import { initProductAnalytics } from "@/lib/productAnalytics";
import { installDomMutationGuard } from "@/lib/domGuard";
import { queryClient } from "@/lib/queryClient";
import { dashboardPages, router } from "./router";

// Before the first render, so a translated page never commits unguarded.
installDomMutationGuard();

// Before the first render, so a boot failure is reported too.
initErrorReporting();
// Off unless the deployment configured a key; a self-host never loads it.
initProductAnalytics();

const rootEl = document.getElementById('root')!
createRoot(rootEl).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <Toaster />
      {import.meta.env.DEV && <ReactQueryDevtools initialIsOpen={false} styleNonce={document.querySelector<HTMLMetaElement>('meta[property="csp-nonce"]')?.nonce} />}
    </QueryClientProvider>
  </StrictMode>,
)

// Reveal once styles and the interface font apply, so text never repaints; the timeouts cap a slow font.
const revealApp = () => rootEl.classList.add('app-ready')
Promise.race([
  document.fonts?.ready ?? Promise.resolve(),
  new Promise((resolve) => setTimeout(resolve, 700)),
]).then(() => requestAnimationFrame(revealApp))
setTimeout(revealApp, 1500)

// Once the dashboard is idle, fetch every page's code, so no navigation ever
// waits on a chunk.
const warmPages = () => {
  for (const load of Object.values(dashboardPages)) void load().catch(() => {})
}
const idle = (cb: () => void) =>
  typeof window.requestIdleCallback === 'function' ? window.requestIdleCallback(cb) : setTimeout(cb, 1200)
let warmed = false
const stopWarming = router.subscribe('onResolved', ({ toLocation }) => {
  if (!warmed && toLocation.pathname.startsWith('/app')) {
    warmed = true
    idle(warmPages)
    stopWarming()
  }
})
