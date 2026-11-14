import { useState } from 'react';

export interface DelegationCardProps {
  toHandle: string;
  status: 'queued' | 'delivered' | 'completed' | 'blocked' | 'failed';
  task: string;
  report?: string;
}

const statusStyle: Record<DelegationCardProps['status'], string> = {
  queued: 'text-zinc-300 border-white/15 bg-white/5',
  delivered: 'text-sky-300 border-sky-400/30 bg-sky-500/10',
  completed: 'text-emerald-300 border-emerald-400/30 bg-emerald-500/10',
  blocked: 'text-amber-300 border-amber-400/30 bg-amber-500/10',
  failed: 'text-red-300 border-red-400/30 bg-red-500/10',
};

export function DelegationCard({ toHandle, status, task, report }: DelegationCardProps) {
  const [open, setOpen] = useState(false);

  return (
    <div className="my-2 rounded-xl border border-white/10 bg-white/[0.03] backdrop-blur-md px-3.5 py-2.5 text-xs">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-between gap-3 text-left"
      >
        <div className="flex items-center gap-2">
          <span className="text-zinc-400">Delegated to</span>
          <span className="font-mono font-medium text-white">@{toHandle}</span>
        </div>
        <span className={`px-2 py-0.5 rounded-md border text-[11px] font-medium ${statusStyle[status]}`}>
          {status === 'delivered' ? 'Working' : status}
        </span>
      </button>

      {open && (
        <div className="mt-2.5 pt-2.5 border-t border-white/10 space-y-2 text-zinc-300">
          <div>
            <div className="text-[11px] uppercase tracking-wider text-zinc-500 mb-0.5">Brief</div>
            <p className="whitespace-pre-wrap leading-relaxed">{task}</p>
          </div>
          {report && (
            <div>
              <div className="text-[11px] uppercase tracking-wider text-zinc-500 mb-0.5">Report</div>
              <p className="whitespace-pre-wrap leading-relaxed">{report}</p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
