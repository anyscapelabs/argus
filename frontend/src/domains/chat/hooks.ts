import { useMemo } from 'react'
import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type InfiniteData } from '@tanstack/react-query'
import { sendMessage, sendMessageWithAttachments, getChat, getUsage, setMessageReaction } from './api'
import type { GetChatResult, ChatMessage } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models'
import { dataOf, mediaTypeOf, type PendingAttachment } from '../uploads/api'
import { messageReaction, patchMessageReaction, rollbackMessageReaction } from './reactions'

export type SendPayload = {
  text: string
  attachmentIDs?: string[]
  attachments?: PendingAttachment[]
}

export function useChatHistory(agentID: string) {
  return useInfiniteQuery({
    queryKey: ['chat', agentID, 'history'],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => getChat(agentID, pageParam as number),
    // Pages arrive newest-first; the cursor is the seq the next (older) page
    // starts strictly below. Zero means every message has been fetched.
    getNextPageParam: (lastPage) => {
      const next = lastPage?.next_before_seq ?? 0
      return next > 0 ? next : undefined
    },
  })
}

// useChatMessages returns the flattened history in display order (ascending).
// Pages map 1:1 onto backend getChat calls, so callers only ever see messages.
export function flattenHistory(data: InfiniteData<GetChatResult> | undefined): ChatMessage[] {
  if (!data) return []
  return data.pages
    .slice()
    .reverse()
    .flatMap((page) => page.messages ?? [])
}

export function useChatMessages(agentID: string) {
  const history = useChatHistory(agentID)
  const messages = useMemo(() => flattenHistory(history.data), [history.data])
  return {
    ...history,
    data: messages,
  }
}

export function useUsage(agentID: string) {
  return useQuery({
    queryKey: ['chat', agentID, 'usage'],
    queryFn: async () => getUsage(agentID),
  })
}

export function useSendMessage(agentID: string) {
  const qc = useQueryClient()
  const messagesKey = ['chat', agentID, 'history']
  const usageKey = ['chat', agentID, 'usage']
  return useMutation({
    mutationFn: ({ text, attachmentIDs }: SendPayload) => {
      if (attachmentIDs && attachmentIDs.length > 0) {
        return sendMessageWithAttachments(agentID, text, attachmentIDs)
      }
      return sendMessage(agentID, text)
    },
    onMutate: async ({ text, attachments }: SendPayload) => {
      await qc.cancelQueries({ queryKey: messagesKey })
      const previous = qc.getQueryData<InfiniteData<GetChatResult>>(messagesKey)
      const stamp = Date.now()
      // The optimistic row rides at the end of the newest page; the real row
      // replaces it when the message event invalidates the history.
      qc.setQueryData<InfiniteData<GetChatResult>>(messagesKey, (old) => {
        if (!old || old.pages.length === 0) return old
        const blocks = (attachments ?? [])
          .filter((a) => a.kind === 'image' && a.dataUrl)
          .map((a) => ({ type: 'image', image: { media_type: mediaTypeOf(a.dataUrl!), data: dataOf(a.dataUrl!) } }))
        const pending: ChatMessage = {
          id: `pending-user-${stamp}`,
          agent_id: agentID,
          seq: 0,
          role: 'user',
          text,
          status: 'complete',
          created_at: stamp,
          blocks,
        } as ChatMessage
        const pages = [...old.pages]
        pages[0] = { ...pages[0], messages: [...(pages[0].messages ?? []), pending] }
        return { ...old, pages }
      })
      return { previous }
    },
    onError: (_err, _payload, ctx) => {
      if (ctx?.previous) qc.setQueryData(messagesKey, ctx.previous)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: messagesKey })
      qc.invalidateQueries({ queryKey: usageKey })
    },
  })
}

export function useSetMessageReaction(agentID: string) {
  const qc = useQueryClient()
  const queryKey = ['chat', agentID, 'history']
  return useMutation({
    mutationFn: ({ messageID, reaction }: { messageID: string; reaction: string }) =>
      setMessageReaction(agentID, messageID, reaction),
    onMutate: async ({ messageID, reaction }) => {
      await qc.cancelQueries({ queryKey })
      const current = qc.getQueryData<InfiniteData<GetChatResult>>(queryKey)
      const previousReaction = messageReaction(current, messageID)
      qc.setQueryData(queryKey, patchMessageReaction(current, messageID, reaction))
      return { messageID, reaction, previousReaction }
    },
    onError: (_error, _variables, context) => {
      if (!context) return
      qc.setQueryData<InfiniteData<GetChatResult>>(queryKey, (current) =>
        rollbackMessageReaction(current, context.messageID, context.reaction, context.previousReaction),
      )
    },
    onSuccess: (result) => {
      qc.setQueryData<InfiniteData<GetChatResult>>(queryKey, (current) =>
        patchMessageReaction(current, result.message_id, result.reaction ?? ''),
      )
    },
  })
}
