import { FiTerminal } from 'react-icons/fi'
import type { PermissionRequestView } from '../../domains/permission/api'

export type Permission = PermissionRequestView

export type PermissionCardProps = {
  permission: Permission | null
  agentName?: string
  busy?: boolean
  onNotify?: (text: string) => void
  onAllowAlways?: () => void
  onAllowOnce?: () => void
  onDeny?: () => void
}

export function present_permission(permission: Permission) {
  return permission
}

export default function PermissionCard({ permission, agentName, busy, onNotify, onAllowAlways, onAllowOnce, onDeny }: PermissionCardProps) {
  if (!permission) return null
  const resolvedAgent = agentName ?? permission.agent_name
  const title = permission.tool == 'execute_command' ? 'Terminal' : permission.tool
  const command = permission.command
  const message =
    resolvedAgent
      ? `Allow ${resolvedAgent} to use the terminal?`
      : 'Allow the agent to use the terminal?'
  const disabled = busy === true

  const click = (notify: string, action?: () => void) => () => {
    if (disabled) return
    onNotify?.(notify)
    action?.()
  }

  return (
    <div className="flex flex-col gap-1 min-h-10 w-full max-w-sm rounded-xl border border-primary bg-secondary p-2">
      <div className="flex items-center gap-2">
        <div className="flex h-4 w-4 items-center justify-center rounded-md border border-secondary">
          <FiTerminal size={9} className="text-primary" />
        </div>
        <span className="text-xs text-primary">{title}</span>
      </div>
      <span className="mt-1 text-sm text-primary">{message}</span>
      <div className="mt-1 rounded-md border border-secondary px-3 py-2 text-xs text-primary">
        {command}
      </div>
      <div className="mt-1 flex justify-end gap-2">
        <button
          type="button"
          disabled={disabled}
          onClick={click(`Denied ${resolvedAgent ?? 'the agent'} access to the terminal`, onDeny)}
          className="cursor-pointer rounded-full bg-red-500 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-50"
        >
          Deny
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={click(`Allowed ${resolvedAgent ?? 'the agent'} to use the terminal once`, onAllowOnce)}
          className="cursor-pointer rounded-full bg-accent px-3 py-1 text-xs font-medium text-black transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          Allow Once
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={click(`Allowed ${resolvedAgent ?? 'the agent'} to use the terminal always`, onAllowAlways)}
          className="cursor-pointer rounded-full bg-accent px-3 py-1 text-xs font-medium text-black transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          Allow Always
        </button>
      </div>
    </div>
  )
}
