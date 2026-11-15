// `@handle` -> markdown link with a private scheme, so react-markdown hands the
// bubble an `a` element it can swap for a MentionChip. Only handles that belong
// to a real agent are rewritten: "@here", a Twitter handle or the `@` inside an
// email address must stay plain text.

export const MENTION_SCHEME = 'mention:'

const NUL = String.fromCharCode(0)

// An @ only starts a mention at the start of the text or after whitespace or an
// opening bracket, which is what keeps `me@example.com` out.
const MENTION = /(^|[\s(>])@([A-Za-z0-9_]+(?:-[A-Za-z0-9_]+)*)/g

export function mentionify(text: string, handles: ReadonlySet<string>): string {
  if (handles.size === 0 || !text.includes('@')) return text
  const stash: string[] = []
  const keep = (value: string) => {
    stash.push(value)
    return `${NUL}${stash.length - 1}${NUL}`
  }
  let out = text
  out = out.replace(/```[\s\S]*?```/g, keep)
  out = out.replace(/`[^`\n]+`/g, keep)
  out = out.replace(/\[[^\]]*\]\([^)]*\)/g, keep)
  out = out.replace(/<[^>\s]+>/g, keep)
  out = out.replace(MENTION, (m, pre: string, handle: string) => {
    const key = handle.toLowerCase()
    return handles.has(key) ? `${pre}[@${handle}](${MENTION_SCHEME}${key})` : m
  })
  const restore = new RegExp(`${NUL}(\\d+)${NUL}`, 'g')
  const resolve = (value: string): string => value.replace(restore, (_m, i: string) => resolve(stash[Number(i)]))
  return resolve(out)
}

export function mentionHandle(href: string | undefined): string | null {
  return href && href.startsWith(MENTION_SCHEME) ? href.slice(MENTION_SCHEME.length) : null
}
