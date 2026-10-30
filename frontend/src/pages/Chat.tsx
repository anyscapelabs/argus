import { useEffect, useMemo, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Events } from '@wailsio/runtime'
import { useAgentProfile } from '../domains/models/hooks'
import { useChatMessages, useSendMessage } from '../domains/chat/hooks'
import { motion, AnimatePresence } from 'framer-motion'
import { FiChevronRight } from 'react-icons/fi'
import Toolbar from '../components/layout/Toolbar'
import ChatInput from '../components/chat/ChatInput'
import AgentBubble from '../components/chat/AgentBubble'
import UserBubble from '../components/chat/UserBubble'
import ThinkingIndicator from '../components/chat/ThinkingIndicator'
import RightPanel from '../components/layout/RightPanel'

function timeTag() {
  const now = new Date()
  const h = now.getHours()
  const m = now.getMinutes().toString().padStart(2, '0')
  const ampm = h >= 12 ? 'pm' : 'am'
  const hour12 = h % 12 || 12
  return `Today at ${hour12}:${m}${ampm}`
}

export default function Chat() {
  const [rightOpen, setRightOpen] = useState(false)
  const { data: agentProfile } = useAgentProfile('default')
  const qc = useQueryClient()
  const { data: messages = [] } = useChatMessages('default')
  const send = useSendMessage('default')
  const tag = useMemo(() => timeTag(), [])

  useEffect(() => {
    const unsubs = [
      Events.On('chat:message', () => {
        qc.invalidateQueries({ queryKey: ['chat', 'default', 'messages'] })
      }),
      Events.On('chat:run_finished', () => {
        qc.invalidateQueries({ queryKey: ['chat', 'default', 'messages'] })
        qc.invalidateQueries({ queryKey: ['chat', 'default', 'usage'] })
      }),
    ]
    return () => unsubs.forEach((u) => u && u())
  }, [qc])

  const handleSend = (text: string, _modelRef: string | null) => {
    send.mutate(text, {
      onError: (err: any) => console.error('send failed', err),
    })
  }

  return (
    <div className="relative flex h-dvh flex-1 min-h-0 overflow-hidden">
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
          <div className="flex-1 min-h-0 overflow-y-auto px-6 py-6">
            <div className="mx-auto flex max-w-3xl flex-col gap-3">
              <div className="mx-auto w-fit rounded-md px-2 py-0.5 text-center text-xs text-secondary transition-colors hover:bg-secondary hover:text-primary">
                {tag}
              </div>
              {messages.map((m: any) => (
                m.role === 'user' ? (
                  <UserBubble key={m.id} content={m.text} />
                ) : (
                  <AgentBubble key={m.id} content={m.text} />
                )
              ))}
              {messages.some((m: any) => m.status === 'streaming') && <ThinkingIndicator />}
            </div>
          </div>
        </div>
        <div className="shrink-0">
          <ChatInput onSend={handleSend} />
        </div>
      </div>
      <AnimatePresence>
        {rightOpen && <RightPanel key="right-panel" onClose={() => setRightOpen(false)} />}
      </AnimatePresence>
    </div>
  )
}