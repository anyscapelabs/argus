import type { DetailedHTMLProps, HTMLAttributes } from 'react'

declare global {
  namespace JSX {
    interface IntrinsicElements {
      'em-emoji': DetailedHTMLProps<HTMLAttributes<HTMLElement>, HTMLElement> & {
        id?: string
        shortcodes?: string
        native?: string
        size?: string | number
        set?: 'native' | 'apple' | 'facebook' | 'google' | 'twitter'
        skin?: string | number
        fallback?: string
      }
    }
  }
}

export {}
