// The live label for a tool the agent has started. The terminal tool is named
// execute_command; every other tool used to fall into the terminal branch and
// read "Running read_file … in the terminal".

const TERMINAL_TOOL = 'execute_command'

export const TERMINAL_WAIT_AFTER_MS = 2500

export const TERMINAL_WAITING_LABEL = 'Waiting for output in the terminal'

const LABELS: Record<string, string> = {
  read_file: 'Reading a file',
  write_file: 'Writing a file',
  edit_file: 'Editing a file',
  grep_files: 'Searching files',
  write_artifact: 'Writing a document',
  create_slides: 'Building slides',
  remember: 'Saving to memory',
  forget: 'Updating memory',
  recall_memories: 'Checking memory',
  conversation_read: 'Reading earlier messages',
  get_time_and_location: 'Checking the time',
  schedule_reminder: 'Scheduling a reminder',
  create_routine: 'Scheduling a routine',
  list_scheduled: 'Checking schedules',
  cancel_scheduled: 'Cancelling a schedule',
  connect_mcp: 'Connecting an integration',
}

export function isTerminalTool(tool: string | undefined): boolean {
  return tool === TERMINAL_TOOL
}

export function terminalRunningLabel(command: string): string {
  const cmd = command.trim()
  if (!cmd) return 'Running a command in the terminal'
  const short = cmd.length > 60 ? cmd.slice(0, 60) + '…' : cmd
  return `Running ${short} in the terminal`
}

export function toolStatusLabel(tool: string | undefined): string {
  if (!tool) return 'Working'
  if (LABELS[tool]) return LABELS[tool]
  // Connector tools arrive as <server>__<tool>.
  if (tool.includes('__')) return 'Using an integration'
  return 'Working'
}
