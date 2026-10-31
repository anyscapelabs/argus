import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Browser } from '@wailsio/runtime'
import { FiGlobe } from 'react-icons/fi'

type WebsiteCardProps = {
  url: string
  children?: ReactNode
}

function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return ''
  }
}

export function isHttpUrl(value: string): boolean {
  return /^https?:\/\//i.test(value.trim())
}

function firstPartyIcon(host: string): string {
  return `https://${host}/favicon.ico`
}

function serviceIcon(host: string): string {
  return `https://www.google.com/s2/favicons?domain=${host}&sz=64`
}

// Inline, text-only link: favicon + underlined site title, flows inside the
// agent's markdown paragraph rather than sitting in its own card.
export default function WebsiteCard({ url, children }: WebsiteCardProps) {
  const host = useMemo(() => hostOf(url), [url])
  const [title, setTitle] = useState('')
  const [icon, setIcon] = useState<string>(() => (host ? firstPartyIcon(host) : ''))
  const [iconExhausted, setIconExhausted] = useState(false)

  useEffect(() => {
    if (!isHttpUrl(url)) return
    let alive = true
    const controller = new AbortController()
    const timer = setTimeout(() => controller.abort(), 5000)
    fetch(url, { signal: controller.signal })
      .then((res) => {
        const type = res.headers.get('content-type') ?? ''
        if (!res.ok || (type !== '' && !type.includes('html'))) return ''
        return res.text()
      })
      .then((html) => {
        if (!alive || !html) return
        const og = html.match(/<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']*)["']/i)
        const raw = og?.[1] ?? html.match(/<title[^>]*>([\s\S]*?)<\/title>/i)?.[1] ?? ''
        const clean = raw.replace(/\s+/g, ' ').trim()
        if (clean) setTitle(clean.slice(0, 120))
      })
      .catch(() => {})
      .finally(() => clearTimeout(timer))
    return () => {
      alive = false
      controller.abort()
    }
  }, [url])

  const childText = typeof children === 'string' ? children.trim() : ''
  const fallback = childText && !isHttpUrl(childText) ? childText : host
  const label = title || fallback || url

  return (
    <a
      href={url}
      target="_blank"
      rel="noreferrer"
      title={url}
      onClick={(e) => {
        e.preventDefault()
        Browser.OpenURL(url).catch(() => {})
      }}
      className="no-drag inline-flex max-w-full items-center gap-1 align-baseline text-inherit"
    >
      {icon && !iconExhausted ? (
        <img
          src={icon}
          alt=""
          loading="lazy"
          className="h-3.5 w-3.5 shrink-0 rounded-[2px]"
          onError={() => {
            if (icon === firstPartyIcon(host)) {
              setIcon(serviceIcon(host))
            } else {
              setIconExhausted(true)
            }
          }}
        />
      ) : (
        <FiGlobe size={13} className="shrink-0 text-secondary" />
      )}
      <span className="inline-block max-w-64 truncate align-baseline underline decoration-grey-500 underline-offset-2 hover:decoration-grey-100">
        {label}
      </span>
    </a>
  )
}
