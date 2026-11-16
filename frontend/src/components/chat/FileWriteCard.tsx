import { FiFilePlus, FiEdit3 } from 'react-icons/fi'
import type { FileWrite } from '../../../bindings/github.com/anyscapelabs/argus/Protocol/models.js'

export default function FileWriteCard({ write }: { write: FileWrite }) {
  const created = write.action === 'created'
  const Icon = created ? FiFilePlus : FiEdit3
  return (
    <div className="w-fit max-w-xl rounded-xl border border-secondary bg-secondary px-3 py-2">
      <div className="flex items-center gap-2">
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-secondary bg-primary">
          <Icon size={15} className="text-secondary" />
        </div>
        <div className="min-w-0">
          <div className="truncate font-mono text-xs text-primary" title={write.path}>{write.path}</div>
          <div className="mt-0.5 text-xs text-secondary">
            {created ? 'Created' : 'Changed'} · {write.bytes} bytes · {write.lines} lines
          </div>
        </div>
      </div>
    </div>
  )
}
