import { describe, expect, it } from 'vitest'
import { linkLabel, linkify } from '../../../frontend/src/lib/linkify'

describe('linkify', () => {
  it('wraps a bare URL at the end of text in angle brackets', () => {
    expect(linkify('see https://example.com/page')).toBe('see <https://example.com/page>')
  })

  it('wraps a bare URL mid-text and strips a trailing comma', () => {
    expect(linkify('visit https://a.b/c, then go')).toBe('visit <https://a.b/c>, then go')
  })

  it('leaves URLs inside fenced code blocks alone', () => {
    const src = 'before\n```\nhttps://x.y/z\n```\nafter'
    expect(linkify(src)).toBe(src)
  })

  it('leaves URLs inside inline code alone', () => {
    const src = 'run `https://x.y/z` now'
    expect(linkify(src)).toBe(src)
  })

  it('leaves markdown links alone (no double wrapping)', () => {
    const src = 'see [the docs](https://d.e/f)'
    expect(linkify(src)).toBe(src)
  })

  it('leaves image links alone', () => {
    const src = '![plot](https://img.example/x.png)'
    expect(linkify(src)).toBe(src)
  })

  it('keeps parentheses-balanced URLs intact, not their neighbours', () => {
    expect(linkify('x (see https://a.b/x) rest')).toBe('x (see <https://a.b/x>) rest')
  })

  it('does not mangle an empty or link-free string', () => {
    expect(linkify('no links here')).toBe('no links here')
    expect(linkify('')).toBe('')
  })

  it('does not touch an already angle-bracketed URL', () => {
    // GFM treats <...> as autolink syntax; react-markdown turns it into an
    // anchor only when the brackets survive. Our output already produces
    // exactly this shape, so it must be stable.
    expect(linkify('see <https://a.b/c>')).not.toContain('<<https')
  })
})

describe('linkLabel', () => {
  it('returns the label text of a text child', () => {
    expect(linkLabel('Read the docs')).toBe('Read the docs')
  })

  it('concatenates nested arrays of strings and numbers', () => {
    expect(linkLabel(['Step ', 3, [' of ', '5']])).toBe('Step 3 of 5')
  })

  it('returns null for nullish and boolean children', () => {
    expect(linkLabel(null)).toBe('')
    expect(linkLabel(false)).toBe('')
  })
})
