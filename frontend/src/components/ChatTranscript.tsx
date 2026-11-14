import { DelegationCard, DelegationCardProps } from './DelegationCard';

export interface TranscriptItem {
  id: string;
  role: 'user' | 'assistant' | 'tool' | 'agent_request' | 'delegation';
  fromHandle?: string;
  content: string;
  delegation?: DelegationCardProps;
}

export function ChatTranscript({ items }: { items: TranscriptItem[] }) {
  return (
    <div className="space-y-3 p-4">
      {items.map((m) => {
        if (m.role === 'delegation' && m.delegation) {
          return <DelegationCard key={m.id} {...m.delegation} />;
        }

        if (m.role === 'agent_request') {
          return (
            <div
              key={m.id}
              className="rounded-xl border border-sky-400/20 bg-sky-500/[0.06] px-3.5 py-2.5 text-xs text-zinc-200"
            >
              <div className="text-[11px] font-mono text-sky-300 mb-1">
                From @{m.fromHandle || 'teammate'}
              </div>
              <p className="whitespace-pre-wrap leading-relaxed">{m.content}</p>
            </div>
          );
        }

        return (
          <div key={m.id} className="text-sm text-zinc-200">
            {m.content}
          </div>
        );
      })}
    </div>
  );
}
