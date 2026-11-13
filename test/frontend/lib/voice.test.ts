import { describe, expect, it } from 'vitest'
import { voiceMessagePreview } from '../../../frontend/src/lib/voice'

describe('voice message preview', () => {
  it('shows the persisted call duration', () => {
    expect(voiceMessagePreview('{"seconds":75,"turns":[]}')).toBe('Voice message · 01:15')
  })

  it('does not expose malformed record text', () => {
    expect(voiceMessagePreview('{broken')).toBe('Voice message')
  })
})
