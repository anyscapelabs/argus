import { useEffect, useState } from 'react'
import { motion } from 'framer-motion'
import { useAgentProfile, useSaveAgentProfile } from '../../domains/models/hooks'
import { GoPencil } from 'react-icons/go'
import { BsLayoutSidebarInset } from 'react-icons/bs'
import { FiImage, FiMoreHorizontal } from 'react-icons/fi'
import { TiDocumentText } from 'react-icons/ti'

type RightPanelProps = {
  onClose?: () => void
  agentName?: string
}

const TABS = ['Details', 'Routines', 'Artifacts']

type Routine = { id: string; name: string; description: string; enabled: boolean }
type Artifact = { id: string; name: string; description: string; kind: string; createdAt: string }

const INITIAL_ROUTINES: Routine[] = [
  { id: '1', name: 'Morning summary', description: 'Daily briefing at 8am', enabled: true },
  { id: '2', name: 'Evening wrap-up', description: 'Summarize the day at 6pm', enabled: false },
  { id: '3', name: 'Weekly review', description: 'Review goals every Friday', enabled: true },
]

const INITIAL_ARTIFACTS: Artifact[] = [
  { id: '1', name: 'report.pdf', description: 'Q3 financial report', kind: 'File', createdAt: '2h ago' },
  { id: '2', name: 'chart.png', description: 'Sales trend chart', kind: 'Image', createdAt: '5h ago' },
  { id: '3', name: 'notes.md', description: 'Meeting notes', kind: 'Doc', createdAt: '1d ago' },
]

export default function RightPanel({ onClose }: RightPanelProps) {
  const { data: profile } = useAgentProfile('default')
  const saveProfile = useSaveAgentProfile()
  const [agentName, setAgentName] = useState('Argus')
  const [active, setActive] = useState(TABS[0])
  const [routines, setRoutines] = useState<Routine[]>(INITIAL_ROUTINES)
  const [artifacts] = useState<Artifact[]>(INITIAL_ARTIFACTS)

  useEffect(() => {
    if (profile?.agent_name) {
      setAgentName(profile.agent_name)
    }
  }, [profile])

  const toggleRoutine = (id: string) => {
    setRoutines((rs) => rs.map((r) => (r.id === id ? { ...r, enabled: !r.enabled } : r)))
  }

  return (
    <motion.aside
      initial={{ x: '100%', opacity: 0 }}
      animate={{ x: 0, opacity: 1 }}
      exit={{ x: '100%', opacity: 0 }}
      transition={{ type: 'tween', duration: 0.2, ease: 'easeOut' }}
      className="flex w-72 shrink-0 flex-col border-l border-primary bg-primary self-stretch"
    >
      <div className="flex items-center p-2">
        <button
          type="button"
          onClick={onClose}
          className="flex h-8 w-8 items-center justify-center rounded-lg text-grey-300 transition-colors hover:bg-hover-primary"
        >
          <BsLayoutSidebarInset size={16} />
        </button>
      </div>
      <div className="flex items-center justify-center py-2">
        <button
          type="button"
          className="group relative grid h-24 w-24 cursor-pointer place-items-center rounded-full border border-primary bg-transparent transition-opacity hover:opacity-80"
        >
          <img src="/logo.png" alt="Argus" className="h-20 w-20 rounded-full transition-opacity group-hover:opacity-0" />
          <GoPencil size={28} className="absolute opacity-0 transition-opacity group-hover:opacity-100" />
        </button>
      </div>
      <div className="flex justify-center px-4">
        <div className="w-fit rounded-lg px-3 py-1.5 text-center transition-colors hover:bg-secondary">
          <input
            type="text"
            value={agentName}
            size={Math.max(1, agentName.length)}
            onChange={(e) => setAgentName(e.target.value)}
            className="bg-transparent text-center text-sm font-medium text-primary outline-none"
          />
          <button
            type="button"
            onClick={() => {
              saveProfile.mutate({
                id: 'default',
                avatar: profile?.avatar ?? '',
                agent_name: agentName,
                model_ref: profile?.model_ref ?? '',
                updated_at: profile?.updated_at ?? '',
              })
            }}
            className="mt-2 rounded-md bg-secondary px-3 py-1 text-xs font-medium text-primary transition-colors hover:bg-hover-secondary"
          >
            Save
          </button>
        </div>
      </div>

      <div className="mt-2 flex flex-wrap justify-center gap-1 px-3 py-2">
        {TABS.map((tab) => (
          <button
            key={tab}
            type="button"
            onClick={() => setActive(tab)}
            className={`rounded-md px-2 py-1 text-sm font-medium transition-colors ${
              active === tab
                ? 'bg-secondary text-primary'
                : 'text-secondary hover:text-primary'
            }`}
          >
            {tab}
          </button>
        ))}
      </div>

      <div className="flex-1 overflow-y-auto px-3 py-2">
        {active === 'Details' && (
          <div className="overflow-hidden rounded-lg border border-secondary bg-secondary">
            <div className="flex items-center justify-between px-3 py-2">
              <span className="text-sm text-secondary">Status</span>
              <span className="text-sm font-medium text-primary">Online</span>
            </div>
            <div className="mx-3 border-t border-secondary" />
            <div className="flex items-center justify-between px-3 py-2">
              <span className="text-sm text-secondary">Last active</span>
              <span className="text-sm font-medium text-primary">Just now</span>
            </div>
            <div className="mx-3 border-t border-secondary" />
            <div className="flex items-center justify-between px-3 py-2">
              <span className="text-sm text-secondary">Uptime</span>
              <span className="text-sm font-medium text-primary">2h 14m</span>
            </div>
          </div>
        )}
        {active === 'Routines' && (
          <div className="overflow-hidden rounded-lg border border-secondary bg-secondary">
            {routines.map((r, i) => (
              <div key={r.id}>
                {i > 0 && <div className="mx-3 border-t border-secondary" />}
                <div className="flex items-center justify-between gap-3 px-3 py-2">
                  <div className="flex flex-col">
                    <span className="text-sm font-medium text-primary">{r.name}</span>
                    <span className="text-sm text-grey-300">{r.description}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => toggleRoutine(r.id)}
                    className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${
                      r.enabled ? 'bg-accent-secondary' : 'bg-tertiary'
                    }`}
                  >
                    <span
                      className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all ${
                        r.enabled ? 'left-4' : 'left-0.5'
                      }`}
                    />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
        {active === 'Artifacts' && (
          <div className="overflow-hidden rounded-lg border border-secondary bg-secondary p-1">
            {artifacts.map((a, i) => (
              <div key={a.id}>
                <div className="group flex items-center gap-1 rounded-lg px-2 py-1 transition-colors hover:bg-tertiary">
                  {a.kind === 'Image' ? (
                    <FiImage size={14} className="text-secondary" />
                  ) : (
                    <TiDocumentText size={14} className="text-secondary" />
                  )}
                  <span className="flex-1 text-sm text-primary">{a.description}</span>
                  <span className="text-xs text-secondary opacity-0 transition-opacity group-hover:opacity-100">
                    {a.createdAt}
                  </span>
                  <button
                    type="button"
                    className="flex h-6 w-6 items-center justify-center rounded-full text-secondary opacity-0 transition-all group-hover:opacity-100 hover:bg-hover-primary hover:text-primary"
                  >
                    <FiMoreHorizontal size={14} />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </motion.aside>
  )
}