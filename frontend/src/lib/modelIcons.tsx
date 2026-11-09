import type { CSSProperties } from 'react'
import { FiCpu } from 'react-icons/fi'

type IconProps = {
  modelId?: string | null
  modelRef?: string | null
  providerId?: string | null
  size?: number
  className?: string
}

const brandSlugs = new Set([
  'anthropic',
  'openai',
  'z-ai',
  'moonshotai',
  'minimax',
  'deepseek',
  'qwen',
  'google',
  'nvidia',
])

function brandFromId(id?: string | null): string | null {
  if (!id) return null
  const normalized = id.toLowerCase()
  const parts = normalized.split('/')
  // refs can be "provider/model/..." (e.g. argus-router/anthropic/claude-...)
  // model ids from catalog are "anthropic/claude-..."
  for (const part of parts) {
    if (brandSlugs.has(part)) return part
  }
  return null
}

export function modelBrandSlug({ modelId, modelRef, providerId }: Pick<IconProps, 'modelId' | 'modelRef' | 'providerId'>): string | null {
  return brandFromId(modelId) ?? brandFromId(modelRef) ?? brandFromId(providerId)
}

export function ModelIcon({ modelId, modelRef, providerId, size = 12, className }: IconProps) {
  const slug = modelBrandSlug({ modelId, modelRef, providerId })
  if (!slug) {
    return <FiCpu size={size} className={className} />
  }
  const style: CSSProperties = {
    width: size,
    height: size,
    backgroundColor: 'currentColor',
    WebkitMaskImage: `url(/icons/models/${slug}.svg)`,
    WebkitMaskRepeat: 'no-repeat',
    WebkitMaskPosition: 'center',
    WebkitMaskSize: 'contain',
    maskImage: `url(/icons/models/${slug}.svg)`,
    maskRepeat: 'no-repeat',
    maskPosition: 'center',
    maskSize: 'contain',
  }
  return <span aria-hidden className={`inline-block shrink-0 ${className ?? ''}`.trim()} style={style} />
}
