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
  return useMutation({
    mutationFn: (text: string) => sendMessage(agentID, text),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['chat', agentID, 'messages'] })
      qc.invalidateQueries({ queryKey: ['chat', agentID, 'usage'] })
    },
  })
}