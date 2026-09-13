import { MD_QUERY, useMediaQuery } from "./useMediaQuery"

// Below Tailwind's `md`, where the sidebar is an off-canvas drawer. Built on
// useMediaQuery so the first render is already correct: the previous
// effect-initialised version reported "not mobile" for one frame, which the
// nav's collapsed rail (a JS branch, not a `md:` class) rendered as a
// label-less drawer.
export function useIsMobile() {
  return !useMediaQuery(MD_QUERY)
}
