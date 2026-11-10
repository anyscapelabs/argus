import { useEffect } from 'react'
import { Events } from '@wailsio/runtime'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  exportArtifact,
  exportDeckPPTX,
  exportFormats,
  getArtifact,
  listArtifacts,
  type ArtifactExportResult,
  type ArtifactView,
  type ExportFormatView,
} from './api'

const listKey = (agentID: string) => ['artifacts', agentID]
const oneKey = (artifactID: string) => ['artifact', artifactID]

// The documents this agent has written. The table is the truth: the agent can
// write one mid-reply, the panel can delete one, and neither is something a
// cached list should be allowed to deny. Every write raises `artifact:updated`,
// which refreshes both the list and whichever document is open in the viewer —
// so a rewrite the agent appends to is on screen without the user closing and
// reopening it.
export function useArtifacts(agentID = 'default') {
  const qc = useQueryClient()

  useEffect(() => {
    const unsub = Events.On('artifact:updated', () => {
      qc.invalidateQueries({ queryKey: listKey(agentID) })
      qc.invalidateQueries({ queryKey: ['artifact'] })
    })
    return () => unsub()
  }, [qc, agentID])

  return useQuery({
    queryKey: listKey(agentID),
    queryFn: () => listArtifacts(agentID),
  })
}

// One document with its body. List deliberately omits the body, so the viewer
// asks for the whole row: that is also what makes it refresh on its own.
export function useArtifact(artifactID?: string | null) {
  return useQuery({
    queryKey: oneKey(artifactID ?? ''),
    queryFn: () => getArtifact(artifactID ?? ''),
    enabled: !!artifactID,
  })
}

// Which exports this machine can actually produce. Chrome and LibreOffice can be
// installed while the app runs, so the answer is asked again every time the
// viewer opens rather than cached for the session.
export function useExportFormats(enabled = true) {
  return useQuery({
    queryKey: ['artifact-formats'],
    queryFn: exportFormats,
    enabled,
    staleTime: 0,
  })
}

// Resolves with the file that landed, or null when the user backed out of the
// save dialog. The dialog itself is part of the call, so a caller cannot export
// without the user naming the destination first.
export function useExportArtifact() {
  return useMutation<ArtifactExportResult | null, Error, {
    artifactID: string
    format: ExportFormatView
    title: string
  }>({
    mutationFn: ({ artifactID, format, title }) => exportArtifact(artifactID, format, title),
  })
}

// Saves a deck's .pptx. Resolves with the file that landed, or null when the
// user backed out of the save dialog.
export function useExportDeck() {
  return useMutation<ArtifactExportResult | null, Error, {
    artifactID: string
    title: string
  }>({
    mutationFn: ({ artifactID, title }) => exportDeckPPTX(artifactID, title),
  })
}

export type { ArtifactExportResult, ArtifactView, ExportFormatView }
