import UserBubble from './UserBubble'
import AgentBubble from './AgentBubble'
import type { VoiceTurn } from '../../domains/live/api'

export default function VoiceTranscript({ turns }: { turns: VoiceTurn[] }) {
  return (
    <div className="flex flex-col gap-3 px-1 py-3">
      {turns.map((t, i) =>
        t.role === 'user' ? (
          <UserBubble key={i} content={t.text} align="left" className="max-w-md" />
        ) : (
          <AgentBubble key={i} content={t.text} className="ml-auto max-w-md" />
        ),
      )}
    </div>
  )
}
