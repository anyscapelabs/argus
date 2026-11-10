import { Dialogs } from '@wailsio/runtime'
import * as ArtifactsService from '../../../bindings/github.com/anyscapelabs/argus/Services/Artifacts/service.js'
import type {
  ArtifactExportResult,
  ArtifactView,
  ExportFormatView,
} from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

// Everything the UI may ask of a document. The bridge is deliberately thin: the
// backend owns validation, versioning and rendering, and this file owns only the
// three things a browser cannot do — unwrap the result envelopes, choose where an
// export lands, and turn a cancelled dialog into "nothing happened".

export async function listArtifacts(agentID: string): Promise<ArtifactView[]> {
  const res = await ArtifactsService.List(agentID)
  if (res?.error) throw new Error(res.error.message)
  // Newest first is what the storage layer returns; the chat needs the opposite,
  // so it sorts for itself and this list stays exactly as the backend ordered it.
  return res.artifacts ?? []
}

export async function getArtifact(artifactID: string): Promise<ArtifactView> {
  const res = await ArtifactsService.Get(artifactID)
  if (res?.error) throw new Error(res.error.message)
  if (!res.found) throw new Error('That document is not in the panel any more.')
  return res.artifact
}

export async function exportFormats(): Promise<ExportFormatView[]> {
  const res = await ArtifactsService.Formats()
  if (res?.error) throw new Error(res.error.message)
  return res.formats ?? []
}

// exportDeckPPTX writes a deck's PowerPoint where the user chose. A deck has no
// menu of formats — it is one file, a .pptx — so this is a single action rather
// than one of several targets.
export async function exportDeckPPTX(
  artifactID: string,
  title: string,
): Promise<ArtifactExportResult | null> {
  const dest = await Dialogs.SaveFile({
    Title: 'Save presentation',
    Filename: `${fileStem(title)}.pptx`,
    CanCreateDirectories: true,
    Filters: [{ DisplayName: 'PowerPoint', Pattern: '*.pptx' }],
  })
  if (!dest) return null
  const res = await ArtifactsService.ExportDeckPPTX(artifactID, dest)
  if (res?.error) throw new Error(res.error.message)
  return res
}

// fileStem is the title made safe to be a filename. Only what would actually
// break a path is removed — spaces and punctuation stay, because a document
// called "Q3 report (draft)" should export as "Q3 report (draft).pdf".
function fileStem(title: string): string {
  const cleaned = title
    .replace(/[\\/:*?"<>|\u0000-\u001f]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 80)
    .trim()
  return cleaned || 'document'
}

// exportArtifact renders the document and writes it where the user just chose.
// The dialog is the consent: the backend is never handed a path the user did not
// name, and a cancelled dialog returns null rather than an error.
export async function exportArtifact(
  artifactID: string,
  format: ExportFormatView,
  title: string,
): Promise<ArtifactExportResult | null> {
  const dest = await Dialogs.SaveFile({
    Title: `Export as ${format.label}`,
    Filename: `${fileStem(title)}.${format.extension}`,
    CanCreateDirectories: true,
    Filters: [{ DisplayName: format.label, Pattern: `*.${format.extension}` }],
  })
  if (!dest) return null
  const res = await ArtifactsService.Export(artifactID, format.id, dest)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export type { ArtifactExportResult, ArtifactView, ExportFormatView }
