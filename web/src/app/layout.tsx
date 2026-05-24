import RippleProvider from "@/hooks/RippleProvider";
import { Outlet } from "react-router-dom";

export default function RootLayout() {
  return (
    <RippleProvider>
      <Outlet />
    </RippleProvider>
  );
}
