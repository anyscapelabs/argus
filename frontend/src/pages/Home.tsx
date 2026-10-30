import { useMemo } from 'react'
import { motion } from 'framer-motion'
import { FiChevronRight } from 'react-icons/fi'
import ChatInput from '../components/chat/ChatInput'

function timeOfDay() {
  const h = new Date().getHours()
  if (h < 12) return 'morning'
  if (h < 18) return 'afternoon'
  return 'evening'
}

export default function Home() {
  const greeting = useMemo(() => `Good ${timeOfDay()}`, [])

  return (
    <div className="relative flex flex-1 min-h-0 flex-col">
      <div className="absolute left-1/2 -top-10 z-10 -translate-x-1/2">
        <motion.button
          type="button"
          transition={{ type: 'spring', stiffness: 300, damping: 20 }}
          className="group flex cursor-pointer items-center gap-2 rounded-full bg-tertiary px-3 py-1 transition-colors hover:bg-hover-secondary"
        >
          <img src="/logo.png" alt="Argus" className="h-8 w-8 rounded-full" />
          <span className="text-sm font-medium text-primary">Argus</span>
          <FiChevronRight size={14} className="w-0 overflow-hidden opacity-0 transition-all duration-300 ease-out group-hover:w-4 group-hover:translate-x-1 group-hover:opacity-100" />
        </motion.button>
      </div>

      <div className="flex-1" />
      <div className="mx-auto w-full max-w-3xl pb-2 text-left">
        <h2 className="text-3xl font-medium text-primary">{greeting}</h2>
        <p className="mb-6 text-lg text-secondary">Let's Chat, How can I help you?</p>
      </div>
      <ChatInput />
    </div>
  )
}