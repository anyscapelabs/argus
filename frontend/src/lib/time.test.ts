import { describe, expect, it } from 'vitest'
import { sidebarTimeLabel } from './time'

const now = new Date(2026, 10, 25, 14, 30).getTime()

describe('sidebarTimeLabel', () => {
  it('uses the clock time for today', () => {
    expect(sidebarTimeLabel(new Date(2026, 10, 25, 9, 5).getTime(), now)).toBe('9:05am')
  })

  it('says Yesterday for the previous day', () => {
    expect(sidebarTimeLabel(new Date(2026, 10, 24, 9, 5).getTime(), now)).toBe('Yesterday')
  })

  it('uses the weekday within the same week', () => {
    expect(sidebarTimeLabel(new Date(2026, 10, 23, 9, 5).getTime(), now)).toBe('Monday')
  })

  it('uses month and day beyond a week', () => {
    expect(sidebarTimeLabel(new Date(2026, 9, 15, 9, 5).getTime(), now)).toBe('Oct 15')
  })

  it('uses a numeric date beyond a year', () => {
    expect(sidebarTimeLabel(new Date(2025, 8, 27, 9, 5).getTime(), now)).toBe('27/09/2025')
  })

  it('is empty when there is no message', () => {
    expect(sidebarTimeLabel(0, now)).toBe('')
  })
})
