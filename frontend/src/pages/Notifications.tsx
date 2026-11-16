import { useEffect, useState } from 'react'

type NotificationSettings = {
  desktop: boolean
  taskComplete: boolean
  agentMessages: boolean
  sounds: boolean
}

const DEFAULTS: NotificationSettings = {
  desktop: true,
  taskComplete: true,
  agentMessages: true,
  sounds: false,
}

function loadSettings(): NotificationSettings {
  try {
    const stored = localStorage.getItem('argus-notifications')
    if (!stored) return DEFAULTS
    const parsed = JSON.parse(stored) as Partial<NotificationSettings>
    return { ...DEFAULTS, ...parsed }
  } catch {
    return DEFAULTS
  }
}

function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean
  onChange: (value: boolean) => void
  label: string
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border transition-colors ${
        checked ? 'border-accent bg-accent' : 'border-secondary bg-secondary'
      }`}
    >
      <span
        className={`inline-block h-3.5 w-3.5 rounded-full bg-accent-foreground transition-transform ${
          checked ? 'translate-x-[18px]' : 'translate-x-0.5'
        }`}
      />
    </button>
  )
}

export default function Notifications() {
  const [settings, setSettings] = useState<NotificationSettings>(loadSettings)

  useEffect(() => {
    localStorage.setItem('argus-notifications', JSON.stringify(settings))
  }, [settings])

  const update = <K extends keyof NotificationSettings>(key: K, value: NotificationSettings[K]) => {
    setSettings((prev) => ({ ...prev, [key]: value }))
  }

  const items: { key: keyof NotificationSettings; title: string; description: string }[] = [
    {
      key: 'desktop',
      title: 'Desktop notifications',
      description: 'Show system notifications when Argus needs your attention.',
    },
    {
      key: 'taskComplete',
      title: 'Task completion',
      description: 'Notify me when a task or background run finishes.',
    },
    {
      key: 'agentMessages',
      title: 'Agent messages',
      description: 'Notify me when an agent sends a new message or reply.',
    },
    {
      key: 'sounds',
      title: 'Sound effects',
      description: 'Play sounds for notifications and key events.',
    },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-primary">
      <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-6 px-6 pb-8">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-medium text-primary">Notifications</h1>
          <p className="text-sm text-secondary">Manage your notification preferences.</p>
        </div>

        <div className="rounded-xl border border-primary">
          {items.map((item, i) => (
            <div key={item.key}>
              <div className="flex items-center justify-between gap-4 px-3 py-2">
                <div className="flex flex-col gap-1">
                  <span className="text-sm font-medium text-primary">{item.title}</span>
                  <span className="text-sm text-secondary">{item.description}</span>
                </div>
                <Toggle
                  checked={settings[item.key]}
                  onChange={(value) => update(item.key, value)}
                  label={item.title}
                />
              </div>
              {i < items.length - 1 && <div className="mx-3 border-b border-primary" />}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}
