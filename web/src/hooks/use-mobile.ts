import * as React from "react"

const MOBILE_BREAKPOINT = 768

export function useMaxWidth(breakpoint: number) {
  const [matches, setMatches] = React.useState(() => window.innerWidth < breakpoint)

  React.useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${breakpoint - 1}px)`)
    const onChange = () => {
      setMatches(window.innerWidth < breakpoint)
    }
    mql.addEventListener("change", onChange)
    setMatches(window.innerWidth < breakpoint)
    return () => mql.removeEventListener("change", onChange)
  }, [breakpoint])

  return !!matches
}

export function useIsMobile() {
  return useMaxWidth(MOBILE_BREAKPOINT)
}
