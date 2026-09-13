import * as React from "react"

// useMediaQuery — a media query as reactive state.
//
// The first value is read synchronously from matchMedia rather than defaulting
// to false in an effect: a layout that keys off it (the unibox contact rail)
// would otherwise render its narrow form for one frame on every wide screen.
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = React.useState(() =>
    typeof window === "undefined" ? false : window.matchMedia(query).matches,
  )

  React.useEffect(() => {
    const mq = window.matchMedia(query)
    const onChange = () => setMatches(mq.matches)
    // Re-read on subscribe: the query can have flipped between the initial
    // render and this effect.
    onChange()
    mq.addEventListener("change", onChange)
    return () => mq.removeEventListener("change", onChange)
  }, [query])

  return matches
}

// The `lg` breakpoint, where the dashboard has room for a third column.
export const LG_QUERY = "(min-width: 1024px)"
