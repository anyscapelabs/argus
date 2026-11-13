import UserBubble from './UserBubble'
import AgentBubble from './AgentBubble'
import type { VoiceTurn } from '../../domains/live/api'

// VoiceTranscript renders a finished call's turns with the very bubbles the
// chat uses, so reading a call back follows the same rules as reading any
// conversation. Voice calls place the user's words left and Argus' right.
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
