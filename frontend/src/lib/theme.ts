import { useCallback, useEffect, useState } from 'react'
import { isPreferenceDirty, loadPreferences, savePreference } from '../domains/preferences/api'

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
const FONT_SIZE_KEY = 'argus-font-size'
const FONT_TYPE_KEY = 'argus-font-type'
const MOTION_KEY = 'argus-motion'

const THEME_MODES = ['system', 'light', 'dark'] as const
const ACCENT_IDS: AccentID[] = ACCENTS.map((a) => a.id)
const FONT_SIZE_IDS = ['small', 'default', 'large'] as const
const FONT_TYPE_IDS = ['default', 'system', 'mono'] as const
const MOTION_IDS = ['system', 'reduced'] as const

function readStored<T extends string>(key: string, allowed: readonly T[], fallback: T): T {
  try {
    const stored = localStorage.getItem(key) as T | null
    return stored && allowed.includes(stored) ? stored : fallback
  } catch {
    return fallback
  }
}

function writeCache(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // localStorage is only the paint cache; SQLite is the source of truth.
  }
}

// useStoredPreference paints from localStorage immediately, then reconciles
// with the durable value from SQLite. Updates write both: the cache for an
// instant response and the backend so the choice survives a restart.
function useStoredPreference<T extends string>(
  key: string,
  allowed: readonly T[],
  fallback: T,
): readonly [T, (next: T) => void] {
  const [value, setValue] = useState<T>(() => readStored(key, allowed, fallback))

  useEffect(() => {
    let live = true
    loadPreferences()
      .then((all) => {
        if (!live || isPreferenceDirty(key)) return
        const stored = all[key]
        if (stored === undefined) return
        const next = allowed.includes(stored as T) ? (stored as T) : fallback
        writeCache(key, next)
        setValue(next)
      })
      .catch((err) => console.warn(`could not load preference ${key}`, err))
    return () => {
      live = false
    }
  }, [key, allowed, fallback])

  const update = useCallback(
    (next: T) => {
      writeCache(key, next)
      setValue(next)
      savePreference(key, next).catch((err) =>
        console.warn(`could not save preference ${key}`, err),
      )
    },
    [key],
  )

  return [value, update] as const
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
  const [mode, setMode] = useStoredPreference(MODE_KEY, THEME_MODES, 'system')

  useEffect(() => {
    applyMode(mode)
    if (mode !== 'system') return
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = () => applyMode('system')
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [mode])

  return [mode, setMode] as const
}

export function useAccent() {
  const [accent, setAccent] = useStoredPreference(ACCENT_KEY, ACCENT_IDS, 'white')

  useEffect(() => {
    applyAccent(accent)
  }, [accent])

  return [accent, setAccent] as const
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
// the body font stack follow automatically) and re-applied on launch from the
// value stored in SQLite.
export function useInterfaceSettings() {
  const [fontSize, setFontSize] = useStoredPreference(FONT_SIZE_KEY, FONT_SIZE_IDS, 'default')
  const [fontType, setFontType] = useStoredPreference(FONT_TYPE_KEY, FONT_TYPE_IDS, 'default')
  const [motion, setMotion] = useStoredPreference(MOTION_KEY, MOTION_IDS, 'system')

  useEffect(() => {
    document.documentElement.style.fontSize = FONT_SIZE_PX[fontSize]
  }, [fontSize])

  useEffect(() => {
    document.documentElement.style.fontFamily = FONT_TYPE_STACK[fontType]
  }, [fontType])

  useEffect(() => {
    document.documentElement.classList.toggle('reduce-motion', motion === 'reduced')
  }, [motion])

  return { fontSize, setFontSize, fontType, setFontType, motion, setMotion }
}
