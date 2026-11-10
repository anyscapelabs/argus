import { useCallback, useEffect, useRef, useState } from 'react'
import { getLiveConfig, conversationRead } from './api'

export type VoiceStatus = 'idle' | 'connecting' | 'live' | 'ended'

// targetRate is the sample rate Gemini Live expects: 16 kHz PCM mono.
const targetRate = 16000
// The single tool the voice model may call. No history is replayed and no other
// tools are attached; when the model needs earlier context it calls this.
const conversationReadTool = {
  functionDeclarations: [
    {
      name: 'conversation_read',
      description:
        'Searches this chat for earlier messages and returns numbered snippets, so you can recall what was said before. Call it when the user references something from earlier in the conversation.',
      parameters: {
        type: 'object',
        properties: {
          query: { type: 'string', description: 'Words to search for in the earlier conversation.' },
        },
        required: ['query'],
      },
    },
  ],
}

// VoiceSystemPrompt is the compact, speech-oriented instruction the live model
// is given. Deliberately no markdown, no tool dump, no history — the model gets
// only this and whatever the user says.
const VoiceSystemPrompt =
  "You are Argus, the user's personal AI assistant, speaking in a live voice call. " +
  'Answer briefly and conversationally, as if talking out loud. Keep it natural and to the point. ' +
  'If the user references something from earlier in the chat, call conversation_read to look it up; ' +
  'otherwise answer from what they just said.'

// base64FromBytes encodes a binary buffer for the wire.
function base64FromBytes(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf)
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

// downsampleTo16k averages groups of samples to reach 16 kHz from whatever the
// capture context runs at. A whole-number ratio is an exact average.
function downsampleTo16k(input: Float32Array, inputRate: number): Int16Array {
  if (inputRate <= 0) inputRate = targetRate
  const ratio = inputRate / targetRate
  if (ratio <= 1) {
    return pcm16(input)
  }
  const group = Math.round(ratio)
  const outLen = Math.floor(input.length / group)
  const out = new Int16Array(outLen)
  for (let i = 0; i < outLen; i++) {
    let sum = 0
    for (let j = 0; j < group; j++) {
      sum += input[i * group + j]
    }
    const v = sum / group
    out[i] = v < 0 ? v * 0x8000 : v * 0x7fff
  }
  return out
}

function pcm16(input: Float32Array): Int16Array {
  const out = new Int16Array(input.length)
  for (let i = 0; i < input.length; i++) {
    const v = Math.max(-1, Math.min(1, input[i]))
    out[i] = v < 0 ? v * 0x8000 : v * 0x7fff
  }
  return out
}

// createAudioCtx opens an AudioContext, preferring the model's 16 kHz rate but
// falling back to the hardware default when the webview rejects it — the
// downsample path handles any input rate. Webviews also start contexts
// suspended until a gesture resumes them, so resume() is fired right away.
function createAudioCtx(): AudioContext {
  const AudioCtx = window.AudioContext || (window as any).webkitAudioContext
  let ctx: AudioContext
  try {
    ctx = new AudioCtx({ sampleRate: targetRate })
  } catch {
    ctx = new AudioCtx()
  }
  void ctx.resume()
  return ctx
}

export type UseLiveCallOptions = {
  agentID?: string
  systemPrompt?: string
  onTranscript?: (text: string) => void
}

export function useLiveCall({ agentID = 'default', systemPrompt = VoiceSystemPrompt }: UseLiveCallOptions = {}) {
  const [status, setStatus] = useState<VoiceStatus>('idle')
  const [elapsed, setElapsed] = useState(0)
  const [error, setError] = useState<string | null>(null)
  // paused mutes the microphone without tearing the call down: no frames leave
  // while paused, and the model simply hears silence until it is lifted.
  const [paused, setPaused] = useState(false)
  // lastSeconds is the duration of the most recently finished call (0 until one
  // ends), so a caller can record it in the transcript when the call is done.
  const [lastSeconds, setLastSeconds] = useState(0)

  const wsRef = useRef<WebSocket | null>(null)
  const micStreamRef = useRef<MediaStream | null>(null)
  const audioCtxRef = useRef<AudioContext | null>(null)
  const processorRef = useRef<ScriptProcessorNode | null>(null)
  const playbackCtxRef = useRef<AudioContext | null>(null)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const startRef = useRef(0)
  const pausedRef = useRef(false)
  const configRef = useRef<{ ws_url: string } | null>(null)
  // frameCountRef caps the one-time server-frame diagnostics to the first few.
  const frameCountRef = useRef(0)
  const systemPromptRef = useRef(systemPrompt)
  systemPromptRef.current = systemPrompt

  const stopTimer = () => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }

  const send = useCallback((frame: unknown) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(frame))
    }
  }, [])

  // scheduleAudio appends a 16 kHz PCM chunk to the playback context, which
  // resamples to the hardware rate.
  const scheduleAudio = useCallback((pcm: ArrayBuffer) => {
    const ctx = playbackCtxRef.current
    if (!ctx) return
    const int16 = new Int16Array(pcm)
    const buffer = ctx.createBuffer(1, int16.length, targetRate)
    const data = buffer.getChannelData(0)
    for (let i = 0; i < int16.length; i++) {
      data[i] = int16[i] / 0x8000
    }
    const src = ctx.createBufferSource()
    src.buffer = buffer
    src.connect(ctx.destination)
    src.start()
  }, [])

  // connectBeep plays the two rising tones that mark the moment the call is
  // connected — and doubles as a playback diagnostic: hearing it proves the
  // audio output path works end to end.
  const connectBeep = useCallback(() => {
    const ctx = playbackCtxRef.current
    if (!ctx) return
    const now = ctx.currentTime
    for (const [freq, at] of [
      [880, 0],
      [1245, 0.16],
    ]) {
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      osc.type = 'sine'
      osc.frequency.value = freq
      gain.gain.setValueAtTime(0, now + at)
      gain.gain.linearRampToValueAtTime(0.18, now + at + 0.02)
      gain.gain.exponentialRampToValueAtTime(0.0001, now + at + 0.15)
      osc.connect(gain)
      gain.connect(ctx.destination)
      osc.start(now + at)
      osc.stop(now + at + 0.17)
    }
  }, [])

  // handleServerFrame processes a message from the model.
  const handleServerFrame = useCallback(
    (raw: string) => {
      let frame: any
      try {
        frame = JSON.parse(raw)
      } catch {
        return
      }
      if (frameCountRef.current < 3) {
        // The first frames say everything about why a call is silent:
        // setupComplete, an error, or audio.
        frameCountRef.current++
        console.debug(`[voice] server frame ${frameCountRef.current}:`, Object.keys(frame).join(','))
      }
      if (frame.setupComplete) {
        setStatus((s) => (s === 'connecting' ? 'live' : s))
        return
      }
      const sc = frame.serverContent
      if (sc?.modelTurn?.parts) {
        for (const part of sc.modelTurn.parts) {
          if (part.inlineData?.data) {
            const bin = atob(part.inlineData.data)
            const bytes = new Uint8Array(bin.length)
            for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
            scheduleAudio(bytes.buffer)
          } else if (part.text) {
            // Model transcript text (also mirrored to onTranscript if wired).
            setStatus((s) => (s === 'connecting' ? 'live' : s))
          } else if (part.functionCall) {
            void handleFunctionCall(part.functionCall)
          }
        }
      }
      if (sc?.turnComplete) {
        setStatus((s) => (s === 'connecting' ? 'live' : s))
      }
      if (frame.error) {
        setError(frame.error.message ?? 'The voice call failed')
        setStatus('ended')
      }
    },
    [scheduleAudio],
  )

  const handleFunctionCall = useCallback(
    async (call: any) => {
      const query = call.name === 'conversation_read' ? String(call.args?.query ?? '').trim() : ''
      let result = ''
      if (call.name === 'conversation_read') {
        try {
          result = await conversationRead(agentID, query)
        } catch (err) {
          result = `could not read the conversation: ${(err as Error).message}`
        }
      } else {
        result = `Unknown tool ${call.name}`
      }
      // Tool answers go back in a toolCall frame keyed by the call's id — that
      // is the wire shape the Live model waits on before it speaks again.
      send({
        toolCall: {
          functionResponses: [
            { id: call.id, response: { output: result } },
          ],
        },
      })
    },
    [agentID, send],
  )

  // attachMic captures the microphone and streams 16 kHz PCM to the model.
  // The processor is pulled through a zero-gain sink: ScriptProcessorNode only
  // runs when wired toward a destination, but connecting it straight to the
  // speakers would play the user's own voice back and the mic would hear it.
  const attachMic = useCallback(
    async (stream: MediaStream) => {
      const ctx = createAudioCtx()
      audioCtxRef.current = ctx
      const source = ctx.createMediaStreamSource(stream)
      const processor = ctx.createScriptProcessor(4096, 1, 1)
      let firstChunk = true
      processor.onaudioprocess = (e) => {
        if (pausedRef.current) return
        const ch = e.inputBuffer.getChannelData(0)
        const pcm = downsampleTo16k(ch, ctx.sampleRate)
        if (firstChunk) {
          firstChunk = false
          console.debug(`[voice] mic streaming at ${ctx.sampleRate}Hz, state=${ctx.state}`)
        }
        send({
          realtimeInput: {
            mediaChunks: [{ mimeType: 'audio/pcm;rate=16000', data: base64FromBytes(pcm.buffer) }],
          },
        })
      }
      const sink = ctx.createGain()
      sink.gain.value = 0
      source.connect(processor)
      processor.connect(sink)
      sink.connect(ctx.destination)
      processorRef.current = processor
    },
    [send],
  )

  const start = useCallback(async () => {
    setError(null)
    setElapsed(0)
    setLastSeconds(0)
    setPaused(false)
    pausedRef.current = false
    setStatus('connecting')
    try {
      if (!configRef.current) {
        configRef.current = await getLiveConfig()
      }
      const cfg = configRef.current
      // The playback context exists before the socket so the connect beep can
      // fire the instant the server accepts the call.
      playbackCtxRef.current = createAudioCtx()
      const ws = new WebSocket(cfg.ws_url)
      // Server frames can arrive as binary blobs as well as text; both carry
      // the same JSON.
      ws.binaryType = 'arraybuffer'
      wsRef.current = ws

      ws.onopen = () => {
        // The router accepted the call — say so out loud.
        connectBeep()
        // No model here on purpose: the router resolves its configured default
        // and injects it into the setup frame before forwarding upstream.
        send({
          setup: {
            generationConfig: {
              responseModalities: ['AUDIO'],
              speechConfig: { voiceConfig: { prebuiltVoiceConfig: { voiceName: 'Kore' } } },
            },
            systemInstruction: { parts: [{ text: systemPromptRef.current }] },
            tools: [conversationReadTool],
          },
        })
      }
      ws.onmessage = (ev) => {
        if (typeof ev.data === 'string') handleServerFrame(ev.data)
        else if (ev.data instanceof ArrayBuffer) handleServerFrame(new TextDecoder().decode(ev.data))
      }
      ws.onerror = () => {
        setError('Could not connect to the voice service')
        setStatus('ended')
      }
      ws.onclose = () => {
        setStatus((s) => (s === 'live' || s === 'connecting' ? 'ended' : s))
        stopTimer()
      }

      // Start the microphone once the socket is open (after setup). A webview
      // without mediaDevices (missing permission plumbing) fails here loudly
      // instead of running a call the model can never hear.
      if (!navigator.mediaDevices?.getUserMedia) {
        throw new Error('Microphone access is not available in this window. Check the app permissions.')
      }
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      micStreamRef.current = stream
      await attachMic(stream)
      startRef.current = Date.now()
      setStatus('live')
      stopTimer()
      timerRef.current = setInterval(() => setElapsed(Math.floor((Date.now() - startRef.current) / 1000)), 250)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not start the voice call')
      setStatus('ended')
      void end()
    }
  }, [attachMic, connectBeep, handleServerFrame, send])

  const end = useCallback(() => {
    stopTimer()
    setLastSeconds(Math.max(0, Math.floor((Date.now() - startRef.current) / 1000)))
    processorRef.current?.disconnect()
    processorRef.current = null
    micStreamRef.current?.getTracks().forEach((t) => t.stop())
    micStreamRef.current = null
    if (audioCtxRef.current) {
      void audioCtxRef.current.close()
      audioCtxRef.current = null
    }
    if (playbackCtxRef.current) {
      void playbackCtxRef.current.close()
      playbackCtxRef.current = null
    }
    if (wsRef.current) {
      wsRef.current.onclose = null
      wsRef.current.close()
      wsRef.current = null
    }
    setStatus('ended')
  }, [])

  // togglePause mutes or unmutes the mic mid-call. The session stays open, so
  // the user can step away from the conversation and rejoin it where it left
  // off instead of hanging up.
  const togglePause = useCallback(() => {
    pausedRef.current = !pausedRef.current
    setPaused(pausedRef.current)
  }, [])

  useEffect(() => () => end(), [end])

  // formatElapsed renders seconds as MM:SS (or H:MM:SS past an hour).
  const display = useCallback(() => {
    const m = Math.floor(elapsed / 60)
    const s = elapsed % 60
    return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  }, [elapsed])

  return { status, elapsed, display, lastSeconds, error, paused, togglePause, start, end }
}
