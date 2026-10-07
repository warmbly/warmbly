// BackgroundLayer — a fixed, pointer-events-none layer behind the entire
// application that displays the user-selected background image and
// responds to blur/opacity preferences stored in the appearance state.
//
// Sits at z-index: 0 behind the entire app shell. Inline styles drive the
// visual values so transitions fire when the user changes settings.
// Toggles the `appearance-bg-active` class on <html> so other surfaces
// can adjust their own opacity when a background is present.

import { useEffect } from 'react'
import { useAppStore } from '@/stores'

const PRESET_GRADIENTS: Record<string, string> = {
  'gradient-1': 'linear-gradient(170deg, #f8fafc 0%, #e0f2fe 18%, #fef3c7 48%, #fde68a 78%, #fefce8 100%)',
  'gradient-4': 'linear-gradient(170deg, #f8fafc 0%, #e0f2fe 18%, #bfdbfe 48%, #93c5fd 78%, #dbeafe 100%)',
  'gradient-5': 'linear-gradient(170deg, #f8fafc 0%, #f1f5f9 25%, #e2e8f0 55%, #cbd5e1 100%)',
}

export function BackgroundLayer() {
  const backgroundPreset = useAppStore((state) => state.backgroundPreset)
  const backgroundImage = useAppStore((state) => state.backgroundImage)
  const backgroundBlur = useAppStore((state) => state.backgroundBlur)
  const backgroundOpacity = useAppStore((state) => state.backgroundOpacity)

  const hasImage = backgroundImage || backgroundPreset !== 'default'

  useEffect(() => {
    const root = document.documentElement
    if (!hasImage) {
      root.classList.remove('appearance-bg-active')
      return
    }
    root.classList.add('appearance-bg-active')
  }, [hasImage])

  const layerStyle: React.CSSProperties = {}
  if (backgroundImage) {
    layerStyle.backgroundImage = `url("${backgroundImage}")`
    layerStyle.backgroundSize = 'cover'
    layerStyle.backgroundPosition = 'center'
    layerStyle.backgroundRepeat = 'no-repeat'
  } else if (backgroundPreset !== 'default' && PRESET_GRADIENTS[backgroundPreset]) {
    layerStyle.background = PRESET_GRADIENTS[backgroundPreset]
  }

  if (hasImage) {
    layerStyle.opacity = backgroundOpacity / 100
    layerStyle.filter = backgroundBlur ? `blur(${backgroundBlur}px)` : undefined
  } else {
    layerStyle.opacity = 1
    layerStyle.filter = undefined
  }

  if (!hasImage) return null

  return <div className="appearance-bg-layer" style={layerStyle} aria-hidden="true" />
}
