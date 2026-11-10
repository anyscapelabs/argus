import * as UploadsService from '../../../bindings/github.com/anyscapelabs/argus/Services/Uploads/service.js'
import type { AttachmentView } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

// Everything the UI may ask of an uploaded file. The bridge is thin: turn the
// file's bytes into base64 (what the backend accepts), unwrap the result
// envelopes, and surface refusals as errors.

export type PendingAttachment = {
  id: string
  filename: string
  kind: string
  dataUrl?: string
}

// mediaTypeOf pulls the mime out of a data URL ("data:image/png;base64,…").
export function mediaTypeOf(dataUrl: string): string {
  const m = /^data:([^;,]+)[;,]/.exec(dataUrl)
  return m ? m[1] : ''
}

// dataOf pulls the bare base64 out of a data URL.
export function dataOf(dataUrl: string): string {
  const comma = dataUrl.indexOf(',')
  return comma >= 0 ? dataUrl.slice(comma + 1) : dataUrl
}

// fileToBase64 reads a browser File as a base64 string for the upload bridge.
function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = String(reader.result ?? '')
      // FileReader gives "data:<mime>;base64,<data>" — the bridge wants the
      // bare base64.
      const comma = result.indexOf(',')
      resolve(comma >= 0 ? result.slice(comma + 1) : result)
    }
    reader.onerror = () => reject(reader.error ?? new Error('Could not read the file'))
    reader.readAsDataURL(file)
  })
}

// uploadFile stores one file and returns its attachment metadata, or throws with
// the backend's reason (bad type, too large).
export async function uploadFile(agentID: string, file: File): Promise<AttachmentView> {
  const data = await fileToBase64(file)
  const res = await UploadsService.Upload(agentID, file.name, file.type || '', data)
  if (res?.error) throw new Error(res.error.message)
  if (!res.attachment) throw new Error('The upload returned nothing')
  return res.attachment
}

export type { AttachmentView }
