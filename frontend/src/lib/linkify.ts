// Markdown link normalization shared by the chat bubbles and the artifact
// viewer. `linkify` wraps bare URLs in angle brackets so react-markdown emits
// real anchors, while keeping code spans/fences and markdown links intact.
// `linkLabel` reduces a rendered link's children to their text label (or null
// for image links).

import { isValidElement, type ReactNode } from 'react'
import type { ReactElement } from 'react'

export function linkify(text: string): string {
  const NUL = String.fromCharCode(0)
  const placeholder = () => new RegExp(`${NUL}(\\d+)${NUL}`, 'g')
  const stash: string[] = []
  const keep = (value: string) => {
    stash.push(value)
    return `${NUL}${stash.length - 1}${NUL}`
  }
  const resolve = (value: string): string =>
    value.replace(placeholder(), (_m, i: string) => resolve(stash[Number(i)]))
  let out = text
  out = out.replace(/```[\s\S]*?```/g, keep)
  out = out.replace(/`[^`\n]+`/g, keep)
  out = out.replace(/!\[[^\]]*\]\([^)]*\)/g, keep)
  out = out.replace(/\[[^\]]+\]\([^)]+\)/g, keep)
  out = out.replace(/(^|[\s(>])(https?:\/\/[^\s<]+)/g, (m, pre: string, url: string, offset: number, string: string) => {
    const clean = url.replace(/[.,;:!?'"]+$/, '')
    if (!clean) return m
    const after = string.slice(offset + m.length)
    if (after === '') {
      return `${pre}<${clean}>`
    }
    if (/^[\s)\],;.!?\n\r]/.test(after)) {
      return `${pre}<${clean}>`
    }
    return m
  })
  return out.replace(placeholder(), (_m, i: string) => resolve(stash[Number(i)]))
}

export function linkLabel(children: ReactNode): string | null {
  let label = ''
  let image = false
  const walk = (node: ReactNode) => {
    if (node == null || typeof node === 'boolean') return
    if (typeof node === 'string' || typeof node === 'number') {
      label += String(node)
      return
    }
    if (Array.isArray(node)) {
      node.forEach(walk)
      return
    }
    if (isValidElement(node)) {
      const el = node as ReactElement<{ src?: string; children?: ReactNode }>
      if (el.type === 'img') {
        image = true
        return
      }
      walk(el.props.children)
    }
  }
  walk(children)
  if (image) return null
  return label
}
