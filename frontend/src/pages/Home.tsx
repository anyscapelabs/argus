import { useMemo, useState } from 'react'
import { useAgentProfile } from '../domains/models/hooks'
import { motion, AnimatePresence } from 'framer-motion'
import { FiChevronRight } from 'react-icons/fi'
import Toolbar from '../components/layout/Toolbar'
import RightPanel from '../components/layout/RightPanel'
import ChatInput from '../components/chat/ChatInput'

function timeOfDay() {
  const h = new Date().getHours()
  if (h < 12) return 'morning'
  if (h < 18) return 'afternoon'
  return 'evening'
}

type HomeProps = {
  onSend?: () => void
}

export default function Home({ onSend }: HomeProps) {
  const greeting = useMemo(() => `Good ${timeOfDay()}`, [])
  const [rightOpen, setRightOpen] = useState(false)
  const { data: agentProfile } = useAgentProfile('default')

  return (
    <div className="flex flex-1 min-h-0">
      <div className="flex flex-1 flex-col min-h-0">
        <Toolbar />
        <div className="relative flex flex-1 min-h-0 flex-col">
          <div className="absolute left-1/2 -top-10 z-10 -translate-x-1/2">
            <motion.button
              type="button"
              onClick={() => setRightOpen((v) => !v)}
              transition={{ type: 'spring', stiffness: 300, damping: 20 }}
              className="group flex cursor-pointer items-center gap-2 rounded-full bg-secondary px-3 py-1 transition-colors hover:bg-hover-secondary"
            >
              <img src="/logo.png" alt="Argus" className="h-8 w-8 rounded-full" />
              <span className="text-sm font-medium text-primary">{agentProfile?.agent_name || 'Argus'}</span>
              <FiChevronRight size={14} className="w-0 overflow-hidden opacity-0 transition-all duration-300 ease-out group-hover:w-4 group-hover:translate-x-1 group-hover:opacity-100" />
            </motion.button>
          </div>

          <div className="flex-1" />
          <div className="mx-auto w-full max-w-3xl pb-2 text-left">
            <h2 className="text-3xl font-medium text-primary">{greeting}</h2>
            <p className="mb-6 text-lg text-secondary">Let's Chat, How can I help you?</p>
          </div>
          <ChatInput onSend={onSend} />
        </div>
      </div>
      <AnimatePresence>
        {rightOpen && <RightPanel key="right-panel" onClose={() => setRightOpen(false)} />}
      </AnimatePresence>
    </div>
  )
}