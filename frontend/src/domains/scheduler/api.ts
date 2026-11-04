import * as SchedulerService from '../../../bindings/github.com/anyscapelabs/argus/Services/Scheduler/service.js'
import type { ScheduledTaskView } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

// Reads and pauses the schedules the agent set. Creation is deliberately
// absent: a routine is a prompt, and writing it is the model's job.
export async function listScheduledTasks(): Promise<ScheduledTaskView[]> {
  const res = await SchedulerService.Tasks()
  if (res?.error) throw new Error(res.error.message)
  return res.tasks ?? []
}

export async function setTaskPaused(taskID: string, paused: boolean) {
  const res = await SchedulerService.SetPaused(taskID, paused)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export type { ScheduledTaskView }
