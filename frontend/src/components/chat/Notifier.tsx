import type { ReactNode } from 'react'

export type NotifierProps = {
  text?: string
  children?: ReactNode
  icon?: ReactNode
  className?: string
}

export default function Notifier({ text, children, icon, className = '' }: NotifierProps) {
  return (
    <div
      className={`mx-auto flex w-fit items-center gap-1.5 rounded-md px-2 py-0.5 text-center text-xs text-secondary transition-colors hover:bg-secondary hover:text-primary ${className}`}
    >
      {icon}
      {children ?? text}
    </div>
  )
}