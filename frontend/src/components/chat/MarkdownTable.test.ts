import { describe, expect, it } from 'vitest'
import { hasMarkdownTable } from './MarkdownTable'

describe('hasMarkdownTable', () => {
  it('detects a delimiter row', () => {
    expect(hasMarkdownTable('| a | b |\n|---|:--:|\n| 1 | 2 |')).toBe(true)
  })

  it('detects a header row while the delimiter is still streaming', () => {
    expect(hasMarkdownTable('Here you go:\n\n| Name | Role |')).toBe(true)
  })

  it('ignores plain text and a single pipe', () => {
    expect(hasMarkdownTable('just text')).toBe(false)
    expect(hasMarkdownTable('a | b')).toBe(false)
  })
})
