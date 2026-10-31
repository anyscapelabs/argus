import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { sendMessage, getChat, getUsage } from './api'

export function useChatMessages(agentID: string) {
  return useQuery({
    queryKey: ['chat', agentID, 'messages'],
    queryFn: async () => {
      const res = await getChat(agentID)
      return res.messages ?? []
    },
  })
}

export function useUsage(agentID: string) {
  return useQuery({
    queryKey: ['chat', agentID, 'usage'],
    queryFn: async () => getUsage(agentID),
  })
}

export function useSendMessage(agentID: string) {
  const qc = useQueryClient()
  const messagesKey = ['chat', agentID, 'messages']
  const usageKey = ['chat', agentID, 'usage']
  return useMutation({
    mutationFn: (text: string) => sendMessage(agentID, text),
    onMutate: async (text: string) => {
      await qc.cancelQueries({ queryKey: messagesKey })
      const previous = qc.getQueryData<unknown[]>(messagesKey)
      const stamp = Date.now()
      qc.setQueryData<unknown[]>(messagesKey, (old = []) => [
        ...old,
        {
          id: `pending-user-${stamp}`,
          agent_id: agentID,
          seq: 0,
          role: 'user',
          text,
          status: 'complete',
          created_at: stamp,
        },
        {
          id: `pending-assistant-${stamp}`,
          agent_id: agentID,
          seq: 0,
          role: 'assistant',
          text: '',
          status: 'streaming',
          created_at: stamp,
        },
      ])
      return { previous }
    },
    onError: (_err, _text, ctx) => {
      if (ctx?.previous) qc.setQueryData(messagesKey, ctx.previous)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: messagesKey })
      qc.invalidateQueries({ queryKey: usageKey })
    },
  })
}