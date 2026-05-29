import RippleProvider from "@/hooks/RippleProvider";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { Outlet } from "react-router-dom";

export default function RootLayout() {
  // Keep the browser tab title in sync with the active route across the whole
  // app (auth, onboarding, dashboard). See useDocumentTitle for the route map.
  useDocumentTitle();

  return (
    <RippleProvider>
      <Outlet />
    </RippleProvider>
  );
}
