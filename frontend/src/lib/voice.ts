export function formatVoiceTime(seconds: number): string {
  const value = Math.max(0, Math.floor(seconds))
  return `${String(Math.floor(value / 60)).padStart(2, '0')}:${String(value % 60).padStart(2, '0')}`
}

// voiceMessagePreview turns a persisted voice row's JSON into the compact text
// used by conversation-list items. Corrupt legacy rows still get a safe label.
export function voiceMessagePreview(text: string): string {
  try {
    const record = JSON.parse(text)
    if (typeof record?.seconds === 'number') {
      return `Voice message · ${formatVoiceTime(record.seconds)}`
    }
  } catch {
    // fall through
  }
  return 'Voice message'
}
