import { useCallback, useEffect, useState } from 'react'

export type ThemeMode = 'system' | 'light' | 'dark'
export type AccentID = 'white' | 'blue' | 'amber' | 'purple'

export type Accent = {
  id: AccentID
  label: string
  swatch: string
  colors: {
    accent: string
    hoverAccent: string
    accentForeground: string
  }
}

export const ACCENTS: Accent[] = [
  {
    id: 'white',
    label: 'White',
    swatch: '#ffffff',
    colors: { accent: '#ffffff', hoverAccent: '#e6e6e8', accentForeground: '#000000' },
  },
  {
    id: 'blue',
    label: 'Blue',
    swatch: '#3B82F6',
    colors: { accent: '#3B82F6', hoverAccent: '#2563EB', accentForeground: '#ffffff' },
  },
  {
    id: 'amber',
    label: 'Amber',
    swatch: '#F59E0B',
    colors: { accent: '#F59E0B', hoverAccent: '#D97706', accentForeground: '#000000' },
  },
  {
    id: 'purple',
    label: 'Purple',
    swatch: '#7B61FF',
    colors: { accent: '#7B61FF', hoverAccent: '#6554C0', accentForeground: '#ffffff' },
  },
]

const MODE_KEY = 'argus-theme'
const ACCENT_KEY = 'argus-accent'

function readStored<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  const stored = localStorage.getItem(key) as T | null
  return stored && allowed.includes(stored) ? stored : fallback
}

// applyAccent writes the accent palette onto the document root. Every accent
// surface in the app is a Tailwind utility over `--color-accent*`, so setting
// the variables here recolors the whole UI in one shot.
function applyAccent(id: AccentID) {
  const accent = ACCENTS.find((a) => a.id === id) ?? ACCENTS[0]
  const root = document.documentElement
  root.style.setProperty('--color-accent', accent.colors.accent)
  root.style.setProperty('--color-hover-accent', accent.colors.hoverAccent)
  root.style.setProperty('--color-accent-secondary', accent.colors.accent)
  root.style.setProperty('--color-hover-accent-secondary', accent.colors.hoverAccent)
  root.style.setProperty('--color-accent-foreground', accent.colors.accentForeground)
  root.dataset.accent = accent.id
}

// applyMode mirrors the previous inline logic in App.tsx: resolve the stored
// mode against the OS preference and expose the result as both a data
// attribute and Tailwind's `dark` class.
function applyMode(mode: ThemeMode) {
  const root = document.documentElement
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  const resolved = mode === 'system' ? (prefersDark ? 'dark' : 'light') : mode
  root.dataset.theme = resolved
  root.classList.toggle('dark', resolved === 'dark')
}

export function useThemeMode() {
  const [mode, setMode] = useState<ThemeMode>(() =>
    readStored(MODE_KEY, ['system', 'light', 'dark'] as const, 'system'),
  )

  useEffect(() => {
    applyMode(mode)
    if (mode !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = () => applyMode('system')
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [mode])

  const update = useCallback((next: ThemeMode) => {
    localStorage.setItem(MODE_KEY, next)
    setMode(next)
  }, [])

  return [mode, update] as const
}

export function useAccent() {
  const [accent, setAccent] = useState<AccentID>(() =>
    readStored(ACCENT_KEY, ACCENTS.map((a) => a.id), 'white'),
  )

  useEffect(() => {
    applyAccent(accent)
  }, [accent])

  const update = useCallback((next: AccentID) => {
    localStorage.setItem(ACCENT_KEY, next)
    setAccent(next)
  }, [])

  return [accent, update] as const
}

export function useAccentSwatch(id: AccentID): string {
  return ACCENTS.find((a) => a.id === id)?.swatch ?? ACCENTS[0].swatch
}

export function accentById(id: AccentID): Accent {
  return ACCENTS.find((a) => a.id === id) ?? ACCENTS[0]
}

export type FontSizeID = 'small' | 'default' | 'large'
export type FontTypeID = 'default' | 'system' | 'mono'
export type MotionID = 'system' | 'reduced'

const FONT_SIZE_KEY = 'argus-font-size'
const FONT_TYPE_KEY = 'argus-font-type'
const MOTION_KEY = 'argus-motion'

const FONT_SIZE_PX: Record<FontSizeID, string> = {
  small: '14px',
  default: '16px',
  large: '18px',
}

const FONT_TYPE_STACK: Record<FontTypeID, string> = {
  default: '"Geist", ui-sans-serif, system-ui, sans-serif',
  system: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif',
  mono: '"Geist Mono Variable", ui-monospace, "SF Mono", Menlo, monospace',
}

// useInterfaceSettings owns the Advanced section of the Appearance page. The
// settings are applied straight onto the root element (rem-based sizing and
// the body font stack follow automatically) and re-applied on launch.
export function useInterfaceSettings() {
  const [fontSize, setFontSize] = useState<FontSizeID>(() =>
    readStored(FONT_SIZE_KEY, ['small', 'default', 'large'] as const, 'default'),
  )
  const [fontType, setFontType] = useState<FontTypeID>(() =>
    readStored(FONT_TYPE_KEY, ['default', 'system', 'mono'] as const, 'default'),
  )
  const [motion, setMotion] = useState<MotionID>(() =>
    readStored(MOTION_KEY, ['system', 'reduced'] as const, 'system'),
  )

  useEffect(() => {
    const root = document.documentElement
    root.style.fontSize = FONT_SIZE_PX[fontSize]
    localStorage.setItem(FONT_SIZE_KEY, fontSize)
  }, [fontSize])

  useEffect(() => {
    const root = document.documentElement
    root.style.fontFamily = FONT_TYPE_STACK[fontType]
    localStorage.setItem(FONT_TYPE_KEY, fontType)
  }, [fontType])

  useEffect(() => {
    const root = document.documentElement
    root.classList.toggle('reduce-motion', motion === 'reduced')
    localStorage.setItem(MOTION_KEY, motion)
  }, [motion])

  return { fontSize, setFontSize, fontType, setFontType, motion, setMotion }
}
