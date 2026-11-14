import { describe, expect, it } from 'vitest'
import {
  TERMINAL_WAITING_LABEL,
  isTerminalTool,
  terminalRunningLabel,
  toolStatusLabel,
} from '../../../frontend/src/components/chat/toolStatus'
import { hasMarkdownTable } from '../../../frontend/src/components/chat/MarkdownTable'

describe('tool status labels', () => {
  it('only the terminal tool is called a terminal', () => {
    expect(isTerminalTool('execute_command')).toBe(true)
    expect(isTerminalTool('read_file')).toBe(false)
    expect(isTerminalTool(undefined)).toBe(false)
  })

  it('names file tools by what they do, not as terminal commands', () => {
    expect(toolStatusLabel('read_file')).toBe('Reading a file')
    expect(toolStatusLabel('edit_file')).toBe('Editing a file')
    expect(toolStatusLabel('read_file')).not.toMatch(/terminal/i)
  })

  it('falls back for connector and unknown tools', () => {
    expect(toolStatusLabel('github__list_prs')).toBe('Using an integration')
    expect(toolStatusLabel('something_new')).toBe('Working')
    expect(toolStatusLabel(undefined)).toBe('Working')
  })

  it('shortens long commands', () => {
    expect(terminalRunningLabel('')).toBe('Running a command in the terminal')
    expect(terminalRunningLabel('npm test')).toBe('Running npm test in the terminal')
    expect(terminalRunningLabel('x'.repeat(100))).toContain('…')
    expect(TERMINAL_WAITING_LABEL).toBe('Waiting for output in the terminal')
  })
})

describe('hasMarkdownTable', () => {
  it('detects a GFM table', () => {
    expect(hasMarkdownTable('Intro\n\n| A | B |\n|---|---|\n| 1 | 2 |')).toBe(true)
    expect(hasMarkdownTable('A | B\n:--- | ---:\n1 | 2')).toBe(true)
  })
  it('ignores prose and horizontal rules', () => {
    expect(hasMarkdownTable('just text')).toBe(false)
    expect(hasMarkdownTable('above\n\n---\n\nbelow')).toBe(false)
  })
})
