import { useEffect, useState } from 'react'
import { motion } from 'framer-motion'
import { useAgentProfile, useSaveAgentProfile } from '../../domains/models/hooks'
import { useScheduledTasks, useSetTaskPaused } from '../../domains/scheduler/hooks'
import { useArtifacts } from '../../domains/artifacts/hooks'
import { useAgentStatus } from '../../domains/status/hooks'
import { lastActiveLabel, nextFireLabel } from '../../lib/time'
import { GoPencil } from 'react-icons/go'
import { BsLayoutSidebarInset } from 'react-icons/bs'
import { FiMoreHorizontal } from 'react-icons/fi'
import { TiDocumentText } from 'react-icons/ti'

type RightPanelProps = {
  onClose?: () => void
  agentName?: string
  // Opens one document in the viewer. The list lives here, but a document is
  // full-screen reading, so the click leaves the panel and the app decides where
  // it lands.
  onOpenArtifact?: (artifactID: string) => void
}

const TABS = ['Details', 'Routines', 'Artifacts']

export default function RightPanel({ onClose, onOpenArtifact }: RightPanelProps) {
  const { data: profile } = useAgentProfile('default')
  const saveProfile = useSaveAgentProfile()
  const { data: routines, isLoading: routinesLoading, error: routinesError } = useScheduledTasks()
  const { data: artifacts, isLoading: artifactsLoading } = useArtifacts('default')
  const { status, now } = useAgentStatus('default')
  const setTaskPaused = useSetTaskPaused()
  const [agentName, setAgentName] = useState('Argus')
  const [active, setActive] = useState(TABS[0])

  useEffect(() => {
    if (profile?.agent_name) {
      setAgentName(profile.agent_name)
    }
  }, [profile])

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
            onBlur={() => {
              saveProfile.mutate({
                id: 'default',
                avatar: profile?.avatar ?? '',
                agent_name: agentName,
                model_ref: profile?.model_ref ?? '',
                updated_at: profile?.updated_at ?? '',
              })
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.currentTarget.blur()
              }
            }}
            className="bg-transparent text-center text-sm font-medium text-primary outline-none"
          />
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
              <span className="text-sm font-medium text-primary">
                {status?.busy ? 'Working' : 'Online'}
              </span>
            </div>
            <div className="mx-3 border-t border-secondary" />
            <div className="flex items-center justify-between px-3 py-2">
              <span className="text-sm text-secondary">Last active</span>
              <span className="text-sm font-medium text-primary">
                {lastActiveLabel(status?.last_active_ms, now)}
              </span>
            </div>
          </div>
        )}
        {active === 'Routines' && (
          <div className="flex flex-1 flex-col gap-2">
            {routinesLoading && (
              <div className="overflow-hidden rounded-lg border border-secondary bg-secondary">
                {[0, 1, 2].map((i) => (
                  <div key={i} className="animate-pulse px-3 py-3">
                    <div className="h-3 w-2/3 rounded bg-tertiary" />
                    <div className="mt-2 h-2.5 w-1/2 rounded bg-tertiary" />
                  </div>
                ))}
              </div>
            )}
            {routinesError && (
              <p className="px-1 text-sm text-secondary">
                Could not read the schedule: {routinesError.message}
              </p>
            )}
            {!routinesLoading && !routinesError && !(routines && routines.length) && (
              <div className="flex flex-1 items-center justify-center">
                <p className="text-sm text-primary">No Routines</p>
              </div>
            )}
            {routines && routines.length > 0 && (
              <div className="overflow-hidden rounded-lg border border-secondary bg-secondary">
                {routines.map((r, i) => (
                  <div key={r.id}>
                    {i > 0 && <div className="mx-3 border-t border-secondary" />}
                    <div className="flex items-center justify-between gap-3 px-3 py-2">
                      <div className="flex min-w-0 flex-col">
                        <span className="truncate text-sm font-medium text-primary">{r.title}</span>
                        {/* A routine is described by its rhythm; a reminder by
                            the thing it has to say. */}
                        <span className="truncate text-sm text-grey-300">
                          {r.kind === 'reminder' ? r.prompt : r.summary}
                        </span>
                        {r.kind === 'reminder' && !r.paused && (
                          <span className="truncate text-xs text-secondary">
                            Reminds {nextFireLabel(r.next_run_at)}
                          </span>
                        )}
                        {!!r.last_status && r.last_status !== 'success' && !r.paused && (
                          <span className="truncate text-xs text-grey-500" title={r.last_status}>
                            Last run did not finish
                          </span>
                        )}
                      </div>
                      <button
                        type="button"
                        disabled={setTaskPaused.isPending}
                        onClick={() => setTaskPaused.mutate({ taskID: r.id, paused: !r.paused })}
                        className={`relative h-5 w-9 shrink-0 rounded-full transition-colors disabled:opacity-60 ${
                          r.paused ? 'bg-tertiary' : 'bg-accent-secondary'
                        }`}
                      >
                        <span
                          className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all ${
                            r.paused ? 'left-0.5' : 'left-4'
                          }`}
                        />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
        {active === 'Artifacts' && (
          <div className="flex flex-col gap-2">
            {artifactsLoading && (
              <div className="overflow-hidden rounded-lg border border-secondary bg-secondary">
                {[0, 1].map((i) => (
                  <div key={i} className="animate-pulse px-3 py-3">
                    <div className="h-3 w-2/3 rounded bg-tertiary" />
                    <div className="mt-2 h-2.5 w-1/2 rounded bg-tertiary" />
                  </div>
                ))}
              </div>
            )}
            {!artifactsLoading && !(artifacts && artifacts.length) && (
              <div className="flex items-center justify-center py-6">
                <p className="text-sm text-primary">No Artifacts</p>
              </div>
            )}
            {artifacts && artifacts.length > 0 && (
              <div className="overflow-hidden rounded-lg border border-secondary bg-secondary p-1">
                {artifacts.map((a) => (
                  <div
                    key={a.id}
                    className="group flex items-center gap-2 rounded-lg px-2 py-1 transition-colors hover:bg-tertiary"
                  >
                    {/* The row opens the document; the title is the button so the
                        trailing controls stay clickable things of their own. */}
                    <button
                      type="button"
                      onClick={() => onOpenArtifact?.(a.id)}
                      className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 text-left"
                    >
                      <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md border border-secondary bg-tertiary">
                        <TiDocumentText size={14} className="text-secondary" />
                      </span>
                      <span className="truncate text-sm text-primary">{a.title}</span>
                    </button>
                    <span className="shrink-0 text-xs text-secondary opacity-0 transition-opacity group-hover:opacity-100">
                      {lastActiveLabel(a.updated_at, now)}
                    </span>
                    <button
                      type="button"
                      className="flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded-full text-secondary opacity-0 transition-all group-hover:opacity-100 hover:bg-hover-primary hover:text-primary"
                    >
                      <FiMoreHorizontal size={14} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </motion.aside>
  )
}