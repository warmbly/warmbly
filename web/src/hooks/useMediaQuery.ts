import * as React from "react"

// useMediaQuery — a media query as reactive state.
//
// The first value is read synchronously from matchMedia rather than defaulting
// to false in an effect: a layout that keys off it (the unibox contact rail,
// the sidebar rail) would otherwise render its narrow form for one frame.
export function useMediaQuery(query: string): boolean {
  const read = React.useCallback(
    () => (typeof window === "undefined" || !window.matchMedia ? false : window.matchMedia(query).matches),
    [query],
  )
  const [matches, setMatches] = React.useState(read)

  React.useEffect(() => {
    const mq = window.matchMedia?.(query)
    if (!mq) return
    const onChange = () => setMatches(mq.matches)
    // Re-read on subscribe: the query can have flipped between the initial
    // render and this effect.
    onChange()
    mq.addEventListener("change", onChange)
    return () => mq.removeEventListener("change", onChange)
  }, [query])

  return matches
}

// Tailwind's breakpoints are rem, and rem in a media query resolves against the
// browser's default font size, not the page's. Spelling these in px would put
// JS and CSS on different lines for anyone who is not on a 16px default, which
// is how a panel ends up in its overlay form while its `lg:` static styles have
// already applied.
export const MD_QUERY = "(min-width: 48rem)"
export const LG_QUERY = "(min-width: 64rem)"
