import { ACCENTS, useAccent, useInterfaceSettings, accentById } from '../lib/theme'
import type { FontSizeID, FontTypeID, MotionID } from '../lib/theme'

const FONT_SIZES: { id: FontSizeID; label: string }[] = [
  { id: 'small', label: 'Small' },
  { id: 'default', label: 'Default' },
  { id: 'large', label: 'Large' },
]

const FONT_TYPES: { id: FontTypeID; label: string }[] = [
  { id: 'default', label: 'Default' },
  { id: 'system', label: 'System' },
  { id: 'mono', label: 'Mono' },
]

const MOTION_OPTIONS: { id: MotionID; label: string }[] = [
  { id: 'system', label: 'System' },
  { id: 'reduced', label: 'Reduced' },
]

export default function Appearance() {
  const [accent, setAccent] = useAccent()
  const { fontSize, setFontSize, fontType, setFontType, motion, setMotion } = useInterfaceSettings()

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-primary">
      <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-6 px-6 pb-8">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-medium text-primary">Appearance</h1>
          <p className="text-sm text-secondary">Choose how Argus looks on your device.</p>
        </div>
        <div className="flex flex-col gap-2">
          <h2 className="text-sm font-medium text-primary">Theme</h2>
          <div className="rounded-xl border border-primary p-4">
            <div className="grid grid-cols-4 gap-3">
              {ACCENTS.map((theme) => (
                <button
                  key={theme.id}
                  type="button"
                  aria-label={`${theme.label} accent`}
                  onClick={() => setAccent(theme.id)}
                  className={`flex aspect-[4/3] cursor-pointer flex-col overflow-hidden rounded-xl border bg-primary p-1 transition-shadow ${
                    accent === theme.id ? 'border-secondary' : 'border-secondary hover:border-tertiary'
                  }`}
                  style={
                    accent === theme.id
                      ? { boxShadow: `0 0 0 2px var(--color-surface), 0 0 0 4px ${theme.swatch}` }
                      : undefined
                  }
                >
                  <div className="flex h-full w-full gap-1.5">
                    <div className="flex w-1/3 flex-col gap-1 rounded-md bg-sidebar p-1.5">
                      <div className="h-1.5 w-full rounded-full bg-tertiary" />
                      <div className="h-1.5 w-full rounded-full" style={{ backgroundColor: theme.swatch }} />
                      <div className="h-1.5 w-2/3 rounded-full bg-tertiary" />
                    </div>
                    <div className="flex flex-1 flex-col gap-1.5 rounded-md bg-hover-primary p-1.5">
                      <div className="h-1.5 w-3/4 rounded-full bg-tertiary" />
                      <div className="h-1.5 w-full rounded-full bg-secondary" />
                      <div className="h-1.5 w-2/3 rounded-full bg-secondary" />
                      <div className="mt-auto flex gap-1">
                        <div className="h-2 w-1/2 rounded-full" style={{ backgroundColor: theme.swatch }} />
                        <div className="h-2 w-1/3 rounded-full bg-tertiary" />
                      </div>
                    </div>
                  </div>
                </button>
              ))}
            </div>
            <p className="mt-3 text-center text-xs text-secondary">{accentById(accent).label}</p>
          </div>
        </div>

        <div className="flex flex-col gap-2">
          <h2 className="text-sm font-medium text-primary">Advanced</h2>
          <div className="rounded-xl border border-primary">
            <div className="flex items-center justify-between gap-4 px-3 py-2">
              <div className="flex flex-col gap-1">
                <span className="text-sm font-medium text-primary">Font size</span>
                <span className="text-sm text-secondary">Adjust the text size across the app.</span>
              </div>
              <SegmentedControl options={FONT_SIZES} value={fontSize} onChange={setFontSize} />
            </div>
            <div className="mx-3 border-b border-primary" />

            <div className="flex items-center justify-between gap-4 px-3 py-2">
              <div className="flex flex-col gap-1">
                <span className="text-sm font-medium text-primary">Font type</span>
                <span className="text-sm text-secondary">Choose the font used in the interface.</span>
              </div>
              <SegmentedControl options={FONT_TYPES} value={fontType} onChange={setFontType} />
            </div>
            <div className="mx-3 border-b border-primary" />

            <div className="flex items-center justify-between gap-4 px-3 py-2">
              <div className="flex flex-col gap-1">
                <span className="text-sm font-medium text-primary">Motion</span>
                <span className="text-sm text-secondary">Control animation and motion effects.</span>
              </div>
              <SegmentedControl options={MOTION_OPTIONS} value={motion} onChange={setMotion} />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { id: T; label: string }[]
  value: T
  onChange: (next: T) => void
}) {
  return (
    <div className="flex items-center gap-1 rounded-full border border-secondary p-1">
      {options.map((opt) => (
        <button
          key={opt.id}
          type="button"
          onClick={() => onChange(opt.id)}
          className={`cursor-pointer rounded-full px-2 py-1 text-sm transition-colors ${
            value === opt.id ? 'bg-secondary text-primary' : 'text-secondary hover:text-primary'
          }`}
        >
          {opt.label}
        </button>
      ))}
    </div>
  )
}
