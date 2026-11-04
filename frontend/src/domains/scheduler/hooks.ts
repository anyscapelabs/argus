import { useEffect } from 'react'
import { Events } from '@wailsio/runtime'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { listScheduledTasks, setTaskPaused, type ScheduledTaskView } from './api'

const tasksKey = ['scheduler', 'tasks']

// Live schedules, soonest fire first. The database owns the truth: the engine
// rewrites next_run_at the moment it fires, so the panel must never cache a
// fire time it computed itself. Every write — the agent's tools, a pause, the
// engine's own advance — raises `schedule:updated`, which is what refreshes
// this query; a panel that is closed simply refetches when it opens again.
export function useScheduledTasks() {
  const qc = useQueryClient()

  useEffect(() => {
    const unsub = Events.On('schedule:updated', () => {
      qc.invalidateQueries({ queryKey: tasksKey })
    })
    return () => unsub()
  }, [qc])

  return useQuery({
    queryKey: tasksKey,
    queryFn: async (): Promise<ScheduledTaskView[]> => {
      const rows = await listScheduledTasks()
      // A reminder that already fired is history, not a schedule: it has no
      // next run, so there is nothing for the panel to offer about it.
      return rows.filter((t) => !t.retired)
    },
  })
}

export function useSetTaskPaused() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ taskID, paused }: { taskID: string; paused: boolean }) =>
      setTaskPaused(taskID, paused),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: tasksKey })
    },
    // A rejected pause must snap the switch back, not leave it lying.
    onError: () => {
      qc.invalidateQueries({ queryKey: tasksKey })
    },
  })
}

export type { ScheduledTaskView }
