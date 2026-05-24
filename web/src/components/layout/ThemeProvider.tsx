import { useEffect } from 'react'
import { useAppStore } from '@/stores'

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const theme = useAppStore((state) => state.theme)
  const setResolvedTheme = useAppStore((state) => state.setResolvedTheme)

  useEffect(() => {
    const root = document.documentElement

    const applyTheme = () => {
      if (theme === 'system') {
        const systemTheme = window.matchMedia('(prefers-color-scheme: dark)').matches
          ? 'dark'
          : 'light'
        root.classList.toggle('dark', systemTheme === 'dark')
        setResolvedTheme(systemTheme)
      } else {
        root.classList.toggle('dark', theme === 'dark')
        setResolvedTheme(theme)
      }
    }

    applyTheme()

    // Listen for system theme changes
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')
    const handleChange = () => {
      if (theme === 'system') {
        applyTheme()
      }
    }

    mediaQuery.addEventListener('change', handleChange)
    return () => mediaQuery.removeEventListener('change', handleChange)
  }, [theme, setResolvedTheme])

  return <>{children}</>
}
