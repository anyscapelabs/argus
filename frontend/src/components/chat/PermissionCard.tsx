import { FiTerminal, FiFolder, FiGlobe, FiPackage } from 'react-icons/fi'
import type { PermissionRequestView } from '../../domains/permission/api'

export type Permission = PermissionRequestView

const TOOL_LABELS: Record<string, string> = {
  execute_command: 'Terminal',
  file_access: 'Files',
  browser_action: 'Browser',
  skill_install: 'Agent Skill',
}

// A browsing grant is scoped to one site and that scope is carried in the tool
// id ("browser_action:reddit.com"), because "always allow the browser" is not a
// decision anybody would make. The label drops the host; the host becomes part of
// the question instead.
export function toolFamily(tool: string): string {
  return tool.split(':')[0]
}

export function toolHost(tool: string): string {
  const i = tool.indexOf(':')
  return i < 0 ? '' : tool.slice(i + 1)
}

export function toolLabel(tool: string): string {
  return TOOL_LABELS[toolFamily(tool)] ?? tool
}

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
  const family = toolFamily(permission.tool)
  const host = toolHost(permission.tool)
  const label = toolLabel(permission.tool)
  const command = permission.command
  const who = resolvedAgent ?? 'the agent'
  const message = host
    ? `Allow ${who} to do this on ${host}?`
    : `Allow ${who} to use ${label.toLowerCase()}?`
  const scope = host ? `on ${host}` : `to use ${label.toLowerCase()}`
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
          {family === 'file_access' ? (
            <FiFolder className="text-primary" size={9} />
          ) : family === 'browser_action' ? (
            <FiGlobe className="text-primary" size={9} />
          ) : family === 'skill_install' ? (
            <FiPackage className="text-primary" size={9} />
          ) : (
            <FiTerminal className="text-primary" size={9} />
          )}
        </div>
        <span className="text-xs text-primary">{label}</span>
      </div>
      <span className="mt-1 text-sm text-primary">{message}</span>
      <div className="mt-1 rounded-md border border-secondary px-3 py-2 text-xs text-primary">
        {command}
      </div>
      <div className="mt-1 flex justify-end gap-2">
        <button
          type="button"
          disabled={disabled}
          onClick={click(`Denied ${who} access ${scope}`, onDeny)}
          className="cursor-pointer rounded-full bg-red-500 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-50"
        >
          Deny
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={click(`Allowed ${who} ${scope} once`, onAllowOnce)}
          className="cursor-pointer rounded-full bg-accent px-3 py-1 text-xs font-medium text-black transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          Allow Once
        </button>
        <button
          type="button"
          disabled={disabled}
          onClick={click(`Allowed ${who} ${scope} always`, onAllowAlways)}
          className="cursor-pointer rounded-full bg-accent px-3 py-1 text-xs font-medium text-black transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          Allow Always
        </button>
      </div>
    </div>
  )
}
