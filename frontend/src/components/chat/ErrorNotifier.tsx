import type { ReactNode } from 'react'
import { IoInformationCircleOutline } from 'react-icons/io5'

export type ErrorNotifierProps = {
  text?: string
  children?: ReactNode
  icon?: ReactNode
  className?: string
}

export default function ErrorNotifier({ text, children, icon, className = '' }: ErrorNotifierProps) {
  return (
    <div
      role="alert"
      className={`mx-auto flex w-fit items-center text-left text-xs text-secondary ${className}`}
    >
      {icon ?? <IoInformationCircleOutline size={18} className="mr-1.5 shrink-0 text-secondary" />}
      {children ?? text}
    </div>
  )
}
