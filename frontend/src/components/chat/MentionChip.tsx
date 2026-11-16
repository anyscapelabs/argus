import AgentAvatar from '../AgentAvatar'

export type MentionAgent = {
  name: string
  handle: string
  avatar?: string | null
}

// An inline `@teammate` reference: the agent's profile picture and name in a
// full-radius pill, sized to sit on a text baseline inside a bubble. It is a
// span, not a link, so the markdown `a` underline never touches it.
export default function MentionChip({ agent }: { agent: MentionAgent }) {
  const label = agent.name || agent.handle
  return (
    <span
      title={`@${agent.handle}`}
      data-mention={agent.handle}
      className="mx-0.5 inline-flex max-w-full items-center gap-1 whitespace-nowrap rounded-full border border-accent-secondary/40 bg-accent-secondary/15 py-0.5 pl-0.5 pr-2 align-middle text-[0.8125rem] font-medium leading-none text-primary"
    >
      <AgentAvatar avatar={agent.avatar} name={label} size={18} />
      <span className="truncate">@{label}</span>
    </span>
  )
}
