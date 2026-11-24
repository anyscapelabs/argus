import { GoArrowUpRight } from 'react-icons/go'
import { FALLBACK_VERSION } from '../domains/updater/api'
import { useCheckForUpdate, useInstallUpdate, useUpdateStatus } from '../domains/updater/hooks'

export default function Updates() {
  const { data: status } = useUpdateStatus()
  const check = useCheckForUpdate()
  const update = useInstallUpdate()
  const version = status?.current || FALLBACK_VERSION
  const hasUpdate = status?.available ?? false

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto p-6">
      <div className="mx-auto w-full max-w-3xl">
        <h1 className="text-2xl font-medium text-primary">This App</h1>
        <p className="mt-2 text-sm text-secondary">Updates and help for this app</p>
        <h2 className="mt-6 text-lg text-primary">Updates</h2>
        <div className="mt-2 flex items-center justify-between gap-4 rounded-lg border border-primary p-4">
          <div className="min-w-0">
            <p className="text-sm font-medium text-primary">Argus Agent {version}</p>
            <p className="mt-0.5 text-xs text-secondary">
              {hasUpdate ? 'Updates available' : 'You are on the latest version known'}
            </p>
          </div>
          {hasUpdate ? (
            <button
              type="button"
              className="shrink-0 rounded-full bg-accent px-4 py-1.5 text-sm text-accent-foreground hover:opacity-90"
              onClick={() => update.mutate()}
            >
              Update
            </button>
          ) : (
            <button
              type="button"
              className="shrink-0 rounded-full bg-accent px-4 py-1.5 text-sm text-accent-foreground hover:opacity-90"
              onClick={() => check.mutate()}
            >
              Check for update
            </button>
          )}
        </div>
        <h2 className="mt-6 text-lg text-primary">Help</h2>
        <div className="mt-2 overflow-hidden rounded-lg border border-primary">
          <button type="button" className="flex w-full items-center justify-between px-4 py-3 text-left text-sm text-primary hover:bg-hover-primary">
            <span>Help Center</span>
            <GoArrowUpRight size={16} className="text-secondary" />
          </button>
          <div className="border-t border-primary" />
          <button type="button" className="flex w-full items-center justify-between px-4 py-3 text-left text-sm text-primary hover:bg-hover-primary">
            <span>Send feedback</span>
            <GoArrowUpRight size={16} className="text-secondary" />
          </button>
          <div className="border-t border-primary" />
          <div className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left text-sm text-primary hover:bg-hover-primary">
            <div className="min-w-0">
              <div>Logs and Diagnostics</div>
              <div className="mt-0.5 text-xs text-secondary">View logs and run diagnostics</div>
            </div>
            <button
              type="button"
              className="shrink-0 rounded-full border border-primary px-3 py-1.5 text-sm text-primary hover:bg-hover-primary"
              onClick={() => {
                // TODO: fetch logs from DB and download as JSON
              }}
            >
              Export
            </button>
          </div>
          <div className="border-t border-primary" />
          <button type="button" className="flex w-full items-center justify-between px-4 py-3 text-left text-sm text-primary hover:bg-hover-primary">
            <span>About Argus Agent</span>
            <GoArrowUpRight size={16} className="text-secondary" />
          </button>
        </div>
      </div>
    </div>
  )
}
