import { useEffect } from 'react'
import { useAppStore } from '@/stores'

// The dashboard is light-only today. This used to mirror the OS preference
// onto a `.dark` root class, but only the CSS-variable components (command
// palette, toasts) have dark styles, so on a dark-mode OS the palette went
// dark inside an all-white app. Force light until a real dark theme ships.
export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const setResolvedTheme = useAppStore((state) => state.setResolvedTheme)

  useEffect(() => {
    document.documentElement.classList.remove('dark')
    setResolvedTheme('light')
  }, [setResolvedTheme])

  return <>{children}</>
}
