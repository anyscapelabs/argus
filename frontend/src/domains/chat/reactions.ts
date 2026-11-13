import type { InfiniteData } from '@tanstack/react-query'
import type { GetChatResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models'

function messageReaction(data: InfiniteData<GetChatResult> | undefined, messageID: string): string {
  for (const page of data?.pages ?? []) {
    const message = (page.messages ?? []).find((item) => item.id === messageID)
    if (message) return message.reaction ?? ''
  }
  return ''
}

export function patchMessageReaction(
  data: InfiniteData<GetChatResult> | undefined,
  messageID: string,
  reaction: string,
): InfiniteData<GetChatResult> | undefined {
  if (!data) return data
  return {
    ...data,
    pages: data.pages.map((page) => ({
      ...page,
      messages: (page.messages ?? []).map((message) => (
        message.id === messageID ? { ...message, reaction } : message
      )),
    })),
  }
}

export function rollbackMessageReaction(
  data: InfiniteData<GetChatResult> | undefined,
  messageID: string,
  attemptedReaction: string,
  previousReaction: string,
): InfiniteData<GetChatResult> | undefined {
  if (messageReaction(data, messageID) !== attemptedReaction) return data
  return patchMessageReaction(data, messageID, previousReaction)
}

export { messageReaction }
