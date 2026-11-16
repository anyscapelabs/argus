import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import EmailDraftCard from './EmailDraftCard'
import MentionChip from './MentionChip'

const draft = { to: ['ana@acme.com'], cc: ['bo@acme.com'], subject: 'Q4 plan', body: 'Hi Ana,\n\nAttached.' }

describe('EmailDraftCard', () => {
  it('shows the draft with a red Deny and an accent Send pill', () => {
    const html = renderToStaticMarkup(<EmailDraftCard draft={draft} agentName="Argus" />)
    expect(html).toContain('ana@acme.com')
    expect(html).toContain('Q4 plan')
    expect(html).toMatch(/rounded-full bg-danger[^>]*>Deny</)
    expect(html).toMatch(/rounded-full bg-accent[^>]*>Send</)
  })

  it('swaps the buttons for the outcome once resolved', () => {
    const sent = renderToStaticMarkup(<EmailDraftCard draft={draft} status="sent" />)
    expect(sent).toContain('Sent')
    expect(sent).not.toContain('>Deny<')
    const denied = renderToStaticMarkup(<EmailDraftCard draft={draft} status="denied" />)
    expect(denied).toContain('Denied')
    expect(denied).not.toContain('>Send<')
  })

  it('disables both buttons while sending', () => {
    const html = renderToStaticMarkup(<EmailDraftCard draft={draft} status="sending" />)
    expect(html).toContain('Sending…')
    expect((html.match(/disabled=""/g) ?? []).length).toBe(2)
  })
})

describe('MentionChip', () => {
  it('renders the picture and name for an image avatar', () => {
    const html = renderToStaticMarkup(<MentionChip agent={{ name: 'Sara', handle: 'sara', avatar: '/sara.png' }} />)
    expect(html).toContain('src="/sara.png"')
    expect(html).toContain('@Sara')
  })

  it('draws the gem for a colour avatar and the initial when there is none', () => {
    expect(renderToStaticMarkup(<MentionChip agent={{ name: 'Sara', handle: 'sara', avatar: '#7B61FF' }} />)).toContain('<svg')
    expect(renderToStaticMarkup(<MentionChip agent={{ name: 'Sara', handle: 'sara', avatar: '' }} />)).toContain('>S<')
  })
})
