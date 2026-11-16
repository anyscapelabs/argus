import { describe, expect, it } from 'vitest'
import { mentionHandle, mentionify } from '../../../frontend/src/lib/mentions'

const handles = new Set(['sara', 'dev-bot'])

describe('mentionify', () => {
  it('rewrites a known handle to a mention link', () => {
    expect(mentionify('ask @sara to review', handles)).toBe('ask [@sara](mention:sara) to review')
  })

  it('matches case-insensitively and keeps the typed label', () => {
    expect(mentionify('cc @Sara.', handles)).toBe('cc [@Sara](mention:sara).')
  })

  it('supports hyphenated handles and a mention at the very start', () => {
    expect(mentionify('@dev-bot, ship it', handles)).toBe('[@dev-bot](mention:dev-bot), ship it')
  })

  it('leaves unknown handles alone', () => {
    expect(mentionify('hey @here and @nobody', handles)).toBe('hey @here and @nobody')
  })

  it('does not touch email addresses', () => {
    expect(mentionify('mail me@sara.com', handles)).toBe('mail me@sara.com')
  })

  it('leaves code spans, fences and existing links alone', () => {
    const src = 'run `@sara` and\n```\n@sara\n```\nsee [@sara](https://x.y)'
    expect(mentionify(src, handles)).toBe(src)
  })

  it('is a no-op without known agents', () => {
    expect(mentionify('@sara', new Set())).toBe('@sara')
  })
})

describe('mentionHandle', () => {
  it('reads the handle from a mention href only', () => {
    expect(mentionHandle('mention:sara')).toBe('sara')
    expect(mentionHandle('https://x.y')).toBeNull()
    expect(mentionHandle(undefined)).toBeNull()
  })
})
