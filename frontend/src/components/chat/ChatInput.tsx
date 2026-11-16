import { useRef, useState, useEffect, useCallback } from 'react'
import { LuPlus } from 'react-icons/lu'
import { FaArrowUp } from 'react-icons/fa'
import { FiCircle, FiX } from 'react-icons/fi'
import ModelPicker from './ModelPicker'
import VoiceIcon from './VoiceIcon'
import { useChatModels, useAgentProfile, useSetAgentModel } from '../../domains/models/hooks'
import { useUsage } from '../../domains/chat/hooks'
import { uploadFile, type PendingAttachment } from '../../domains/uploads/api'
import { ModelIcon } from '../../lib/modelIcons'

function formatUsage(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}K`
  return `${n}`
}

// maxUploadBytes matches the backend cap, so an oversized file is caught here
// before it is even sent.
const maxUploadBytes = 10 << 20

const acceptedMimes = [
  'image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/bmp',
  'application/pdf',
  'application/msword',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'text/plain', 'text/markdown',
]

function previewUrl(file: File): string {
  return URL.createObjectURL(file)
}

type ChatInputProps = {
  agentID?: string
  onSend?: (text: string, modelRef: string | null, attachments: PendingAttachment[]) => void
  busy?: boolean
  onStartVoice?: () => void
  voiceActive?: boolean
}

export default function ChatInput({ agentID = 'default', onSend, busy = false, onStartVoice, voiceActive = false }: ChatInputProps) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)
  const fileRef = useRef<HTMLInputElement | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [modelOpen, setModelOpen] = useState(false)
  const [selectedRef, setSelectedRef] = useState<string | null>(null)
  const [text, setText] = useState('')
  const [files, setFiles] = useState<File[]>([])
  const [attachError, setAttachError] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)
  const { data: chatModelsData } = useChatModels()
  const { data: agentProfile } = useAgentProfile(agentID)
  const setAgentModel = useSetAgentModel()
  const { data: usage } = useUsage(agentID)
  const chatModels = chatModelsData ?? []

  const initialized = useRef(false)
  useEffect(() => {
    if (initialized.current) return
    if (agentProfile === undefined || chatModelsData === undefined) return
    if (agentProfile?.model_ref) {
      setSelectedRef(agentProfile.model_ref)
    } else if (chatModels.length > 0) {
      setSelectedRef(chatModels[0].ref)
      setAgentModel.mutate({ id: agentID, modelRef: chatModels[0].ref })
    }
    initialized.current = true
  }, [agentID, agentProfile, chatModelsData])

  const selectedModel = chatModels.find((m) => m.ref === selectedRef)
  const displayName = selectedModel?.model?.name ?? selectedRef ?? 'Select model'

  const handleSelectModel = (ref: string) => {
    setSelectedRef(ref)
    setAgentModel.mutate({ id: agentID, modelRef: ref })
  }

  const addFiles = useCallback((incoming: File[]) => {
    setAttachError(null)
    const valid: File[] = []
    for (const f of incoming) {
      if (!acceptedMimes.includes(f.type)) {
        setAttachError(`${f.name}: this file type isn't supported. Use an image (png, jpg, gif, webp, bmp) or a document (pdf, docx, txt, md).`)
        continue
      }
      if (f.size > maxUploadBytes) {
        setAttachError(`${f.name} is over the 10 MB limit.`)
        continue
      }
      valid.push(f)
    }
    if (valid.length) setFiles((prev) => [...prev, ...valid])
  }, [])

  const handlePick = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) addFiles(Array.from(e.target.files))
    e.target.value = ''
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    if (e.dataTransfer.files) addFiles(Array.from(e.dataTransfer.files))
  }

  const handlePaste = (e: React.ClipboardEvent) => {
    const items = Array.from(e.clipboardData?.items ?? [])
    const imgs = items
      .filter((i) => i.type.startsWith('image/'))
      .map((i) => i.getAsFile())
      .filter((f): f is File => !!f)
    if (imgs.length) {
      e.preventDefault()
      addFiles(imgs)
    }
  }

  const removeFile = (index: number) => {
    setFiles((prev) => prev.filter((_, i) => i !== index))
  }

  const handleSend = async () => {
    if (busy || uploading) return
    const value = text.trim()
    if (!value && files.length === 0) return
    setUploading(true)
    setAttachError(null)
    try {
      // Upload every attached file, then send. Uploading happens here so the
      // backend has the rows it needs to fold into the message; the optimistic
      // row below reuses these for its image previews.
      const uploaded: PendingAttachment[] = []
      for (const file of files) {
        const att = await uploadFile(agentID, file)
        uploaded.push({
          id: att.id,
          filename: att.filename,
          kind: att.kind,
          dataUrl: att.data_url,
        })
      }
      onSend?.(value, selectedRef, uploaded)
      setFiles([])
      setText('')
      if (textareaRef.current) {
        textareaRef.current.style.height = 'auto'
      }
      setExpanded(false)
    } catch (err) {
      setAttachError(err instanceof Error ? err.message : 'Could not upload the file')
    } finally {
      setUploading(false)
    }
  }

  const resize = () => {
    const el = textareaRef.current
    if (!el) return
    // Everything is sized in rem so the pill keeps its shape at any font-size
    // setting: the cap is 10rem and "expanded" means the content is taller
    // than one line (line height + vertical padding), both resolved from the
    // live computed styles rather than hardcoded pixels.
    const root = parseFloat(getComputedStyle(document.documentElement).fontSize)
    const cs = getComputedStyle(el)
    const baseline = Math.ceil(
      parseFloat(cs.lineHeight) + parseFloat(cs.paddingTop) + parseFloat(cs.paddingBottom),
    )
    el.style.height = 'auto'
    const next = Math.min(el.scrollHeight, root * 10)
    el.style.height = `${next}px`
    setExpanded(el.scrollHeight > baseline + 1)
  }

  return (
    <div className="mx-auto mb-2 flex w-3xl flex-col gap-1">
      {files.length > 0 && (
        <div className="flex flex-wrap gap-2 px-2">
          {files.map((f, i) => (
            <div
              key={i}
              className="group relative flex items-center gap-2 rounded-lg border border-secondary bg-secondary px-2 py-1 text-xs text-primary"
            >
              {f.type.startsWith('image/') ? (
                <img src={previewUrl(f)} alt={f.name} className="h-8 w-8 rounded object-cover" />
              ) : (
                <span className="rounded bg-accent px-1 py-0.5 text-[10px] font-semibold uppercase text-accent-foreground">
                  {f.name.split('.').pop()}
                </span>
              )}
              <span className="max-w-32 truncate">{f.name}</span>
              <button
                type="button"
                aria-label={`Remove ${f.name}`}
                onClick={() => removeFile(i)}
                className="text-secondary transition-colors hover:text-primary"
              >
                <FiX className="size-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}
      <div
        className={`flex flex-col border border-primary bg-transparent ${expanded ? 'rounded-2xl' : 'rounded-full'}`}
      >
        {attachError && <div className="px-3 pt-2 text-xs text-danger">{attachError}</div>}
        <div className="flex items-end p-1">
          <input ref={fileRef} type="file" multiple hidden onChange={handlePick} />
          <button
            type="button"
            aria-label="Attach a file"
            onClick={() => fileRef.current?.click()}
            className="flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-full bg-secondary text-primary transition-colors hover:bg-hover-secondary"
          >
            <LuPlus className="size-3.5" />
          </button>
          <textarea
            ref={textareaRef}
            rows={1}
            value={text}
            onDrop={handleDrop}
            onPaste={handlePaste}
            onChange={(e) => {
              setText(e.target.value)
              resize()
            }}
            onInput={resize}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault()
                handleSend()
              }
            }}
            placeholder={files.length ? 'Add a message…' : 'Message…'}
            className="mx-2 min-h-7 max-h-40 flex-1 resize-none overflow-hidden bg-transparent py-1 text-sm leading-5 text-primary placeholder:text-grey-300 focus:outline-none"
          />
          {text.trim() || files.length ? (
            <button
              type="button"
              onClick={handleSend}
              disabled={busy || uploading}
              aria-label="Send message"
              className={`flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-full bg-accent text-accent-foreground transition-colors hover:bg-hover-accent disabled:cursor-not-allowed ${
                busy || uploading ? 'opacity-50' : ''
              }`}
            >
              <FaArrowUp className="size-3" />
            </button>
          ) : (
            <button
              type="button"
              aria-label="Start voice call"
              disabled={voiceActive}
              onClick={() => onStartVoice?.()}
              className="flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-full bg-accent text-accent-foreground transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-50"
            >
              <VoiceIcon className="size-4.5" />
            </button>
          )}
        </div>
      </div>
      <div className="relative flex items-center justify-between pr-2">
        <span className="flex cursor-pointer items-center gap-1 rounded-md px-2 py-1 text-xs text-secondary transition-colors hover:bg-hover-secondary hover:text-primary">
          <FiCircle className="size-3" />
          {usage && usage.requests ? formatUsage((usage.input_tokens ?? 0) + (usage.output_tokens ?? 0)) : '—'} tokens
        </span>
        <button
          type="button"
          onClick={() => setModelOpen((v) => !v)}
          className={`flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors ${
            modelOpen
              ? 'bg-secondary text-primary'
              : 'text-secondary hover:bg-hover-secondary hover:text-primary'
          }`}
        >
          <ModelIcon
            modelId={selectedModel?.model?.id}
            modelRef={selectedRef}
            providerId={selectedModel?.provider_id}
            size={12}
          />
          {displayName}
        </button>
        {modelOpen && (
          <div className="absolute bottom-full right-2 mb-1">
            <ModelPicker selectedRef={selectedRef} onSelect={handleSelectModel} onClose={() => setModelOpen(false)} />
          </div>
        )}
      </div>
    </div>
  )
}
