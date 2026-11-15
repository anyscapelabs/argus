import AgentGem, { isAvatarColor } from './AgentGem'

type AgentAvatarProps = {
  avatar?: string | null
  name: string
  // Outer diameter in px. The gem / image sits inside with a 1px ring.
  size?: number
  className?: string
}

// One place that knows how an agent's `avatar` value is drawn: a hex colour
// becomes the gem, anything else is an image path, and an empty value falls
// back to the agent's initial so a mention never renders a broken image.
export default function AgentAvatar({ avatar, name, size = 16, className = '' }: AgentAvatarProps) {
  const dim = { width: size, height: size }
  return (
    <span
      className={`inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full bg-primary ${className}`}
      style={dim}
      aria-hidden="true"
    >
      {isAvatarColor(avatar) ? (
        <AgentGem color={avatar as string} className="h-full w-full p-px" />
      ) : avatar ? (
        <img src={avatar} alt="" className="h-full w-full rounded-full object-cover" draggable={false} />
      ) : (
        <span className="font-medium uppercase text-secondary" style={{ fontSize: Math.max(8, size * 0.5) }}>
          {name.trim().charAt(0) || '?'}
        </span>
      )}
    </span>
  )
}
