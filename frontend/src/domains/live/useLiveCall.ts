import { useCallback, useEffect, useRef, useState } from 'react'
import { getLiveConfig, conversationRead } from './api'

export type VoiceStatus = 'idle' | 'connecting' | 'live' | 'ended'

// targetRate is the sample rate Gemini Live expects for INPUT: 16 kHz PCM mono.
const targetRate = 16000
// outputRate is the sample rate Gemini Live sends back: 24 kHz PCM mono. Playing
// it as 16 kHz makes the voice slow and deep.
const outputRate = 24000
// playbackLeadSec is the small head start given to model audio whenever playback
// (re)starts from silence. Chunks arrive over a websocket with uneven timing; with
// no lead, a chunk that lands a few milliseconds late leaves a hole in the
// speech — heard as a word clipped off mid-sentence.
const playbackLeadSec = 0.15
// echoGuardTailSec keeps the mic guard up briefly after the model stops
// speaking, so the room's reverb of its last words is not heard as the user.
const echoGuardTailSec = 0.3
// bargeInRMS is the loudness (0..1) the mic must reach WHILE the model is
// speaking before it is forwarded as real speech. Speaker echo that leaks past
// the webview's echo canceller stays below it; a user talking over the model
// does not. Raise it if the model still cuts itself off, lower it if barge-in
// stops working.
const bargeInRMS = 0.05
// voiceName is the prebuilt Gemini voice used for every call.
const voiceName = 'Alnilam'
// The single tool the voice model may call. No history is replayed and no other
// tools are attached; when the model needs earlier context it calls this.
// Gemini 3.8 Live defaults to asynchronous (NON_BLOCKING) function calling; the
// model keeps talking while the tool runs and picks the result up when it lands.
const conversationReadTool = {
  functionDeclarations: [
    {
      name: 'conversation_read',
      description:
        'Searches this chat for earlier messages and returns numbered snippets, so you can recall what was said before. Call it when the user references something from earlier in the conversation.',
      behavior: 'NON_BLOCKING',
      parameters: {
        type: 'OBJECT',
        properties: {
          query: { type: 'STRING', description: 'Words to search for in the earlier conversation.' },
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

// rms is the root-mean-square loudness of a mic buffer, 0 (silence) to 1.
function rms(input: Float32Array): number {
  let sum = 0
  for (let i = 0; i < input.length; i++) sum += input[i] * input[i]
  return Math.sqrt(sum / Math.max(1, input.length))
}

// createAudioCtx opens an AudioContext at the requested rate (16 kHz for the mic,
// 24 kHz for playback so the model's audio is not resampled down), falling back
// to the hardware default when the webview rejects it — the downsample path
// handles any input rate. Webviews also start contexts suspended until a gesture
// resumes them, so resume() is fired right away.
function createAudioCtx(rate: number = targetRate): AudioContext {
  const AudioCtx = window.AudioContext || (window as any).webkitAudioContext
  let ctx: AudioContext
  try {
    ctx = new AudioCtx({ sampleRate: rate })
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
  // setupDoneRef flips once Google answers the setup frame with setupComplete.
  // No audio may leave before then: the session does not exist upstream yet.
  const setupDoneRef = useRef(false)
  // nextPlayTimeRef/playingRef queue model audio back to back instead of
  // starting every chunk at once, and let an interruption cut playback cleanly.
  const nextPlayTimeRef = useRef(0)
  const playingRef = useRef<Set<AudioBufferSourceNode>>(new Set())
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

  // scheduleAudio queues a 24 kHz PCM chunk (the model's output rate) on the
  // playback context, which resamples to the hardware rate. Chunks are chained
  // on a running clock so they play in order without overlapping.
  const scheduleAudio = useCallback((pcm: ArrayBuffer) => {
    const ctx = playbackCtxRef.current
    if (!ctx) return
    // Whole 16-bit samples only; a stray odd byte would make Int16Array throw.
    const int16 = new Int16Array(pcm, 0, Math.floor(pcm.byteLength / 2))
    if (int16.length === 0) return
    const buffer = ctx.createBuffer(1, int16.length, outputRate)
    const data = buffer.getChannelData(0)
    for (let i = 0; i < int16.length; i++) {
      data[i] = int16[i] / 0x8000
    }
    const src = ctx.createBufferSource()
    src.buffer = buffer
    src.connect(ctx.destination)
    // Keep chaining onto the running clock while audio is queued; if playback
    // has run dry (first chunk of a reply, or an underrun) restart a beat ahead
    // so the next late chunk has room to land instead of leaving a gap.
    const now = ctx.currentTime
    let startAt = nextPlayTimeRef.current
    if (startAt <= now) startAt = now + playbackLeadSec
    src.start(startAt)
    nextPlayTimeRef.current = startAt + buffer.duration
    playingRef.current.add(src)
    src.onended = () => playingRef.current.delete(src)
  }, [])

  // flushAudio drops everything queued for playback. Google sends
  // serverContent.interrupted when the user barges in; the model has stopped
  // generating, so the audio already queued must stop too.
  const flushAudio = useCallback(() => {
    for (const src of playingRef.current) {
      try {
        src.stop()
      } catch {
        // already stopped
      }
    }
    playingRef.current.clear()
    nextPlayTimeRef.current = 0
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

  // handleFunctionCall runs one tool the model asked for and answers it.
  //
  // Wire contract (Gemini Live): the model asks for tools in a TOP-LEVEL
  // `toolCall.functionCalls` frame — never inside serverContent.modelTurn — and
  // the client answers with a `toolResponse` frame whose entries carry the call's
  // `id` AND its `name`. Anything else leaves the model waiting for a result that
  // never arrives, which from the outside looks like a call that went silent.
  const handleFunctionCall = useCallback(
    async (call: { id: string; name: string; args?: Record<string, unknown> }) => {
      let result = ''
      if (call.name === 'conversation_read') {
        const query = String(call.args?.query ?? '').trim()
        try {
          result = await conversationRead(agentID, query)
        } catch (err) {
          result = `could not read the conversation: ${(err as Error).message}`
        }
      } else {
        result = `Unknown tool ${call.name}`
      }
      send({
        toolResponse: {
          functionResponses: [{ id: call.id, name: call.name, response: { output: result } }],
        },
      })
    },
    [agentID, send],
  )

  // handleServerFrame processes a message from the model.
  const handleServerFrame = useCallback(
    (raw: string) => {
      let frame: any
      try {
        frame = JSON.parse(raw)
      } catch {
        return
      }
      if (frameCountRef.current < 5) {
        // The first frames say everything about why a call is silent:
        // setupComplete, an error, or audio.
        frameCountRef.current++
        console.debug(`[voice] server frame ${frameCountRef.current}:`, Object.keys(frame).join(','))
      }
      if (frame.setupComplete) {
        // The upstream session now exists: the mic may start sending, and this is
        // the moment the call has actually connected, so the clock starts here
        // (not while the socket is still opening).
        setupDoneRef.current = true
        startRef.current = Date.now()
        setElapsed(0)
        if (timerRef.current) clearInterval(timerRef.current)
        timerRef.current = setInterval(() => setElapsed(Math.floor((Date.now() - startRef.current) / 1000)), 250)
        setStatus((s) => (s === 'connecting' ? 'live' : s))
        return
      }
      if (frame.toolCall?.functionCalls) {
        for (const call of frame.toolCall.functionCalls) {
          void handleFunctionCall(call)
        }
      }
      const sc = frame.serverContent
      if (sc?.interrupted) {
        flushAudio()
      }
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
    [scheduleAudio, flushAudio, handleFunctionCall],
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
        // Hold audio until Google confirms the session with setupComplete; frames
        // sent before that point are not part of a live session yet.
        if (pausedRef.current || !setupDoneRef.current) return
        const ch = e.inputBuffer.getChannelData(0)
        const pcm = downsampleTo16k(ch, ctx.sampleRate)
        if (firstChunk) {
          firstChunk = false
          console.debug(`[voice] mic streaming at ${ctx.sampleRate}Hz, state=${ctx.state}`)
        }
        // Echo guard. While the model is talking (plus a short tail) the speakers
        // feed its own voice back into the mic; the server hears that as the user
        // barging in, cuts the reply off mid-word, and then picks it back up. Quiet
        // mic audio in that window is replaced with silence so the stream stays
        // continuous but carries no echo; a user speaking over the model is loud
        // enough to pass and still interrupts it.
        const playback = playbackCtxRef.current
        const modelSpeaking = !!playback && playback.currentTime < nextPlayTimeRef.current + echoGuardTailSec
        if (modelSpeaking && rms(ch) < bargeInRMS) {
          pcm.fill(0)
        }
        // Gemini 3.x takes mic audio in `realtimeInput.audio`. The legacy
        // `mediaChunks` array is deprecated and must not be used.
        send({
          realtimeInput: {
            audio: { mimeType: 'audio/pcm;rate=16000', data: base64FromBytes(pcm.buffer) },
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
    setupDoneRef.current = false
    startRef.current = 0
    stopTimer()
    frameCountRef.current = 0
    flushAudio()
    setStatus('connecting')
    try {
      if (!configRef.current) {
        configRef.current = await getLiveConfig()
      }
      const cfg = configRef.current
      // The playback context exists before the socket so the connect beep can
      // fire the instant the server accepts the call. It runs at the model's
      // 24 kHz output rate so the voice is not resampled down on the way out.
      playbackCtxRef.current = createAudioCtx(outputRate)
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
              speechConfig: { voiceConfig: { prebuiltVoiceConfig: { voiceName } } },
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
      ws.onclose = (ev) => {
        // Google's own reason for hanging up (bad frame, bad model, auth…) rides
        // in the close code/reason; without this a rejected call just goes quiet.
        console.warn(`[voice] socket closed code=${ev.code} reason=${ev.reason || '(none)'}`)
        if (ev.code !== 1000 && ev.code !== 1005) {
          // Keep the first error if one was already reported (it is more specific).
          setError((prev) => prev ?? `The voice service closed the call (${ev.code}${ev.reason ? `: ${ev.reason}` : ''})`)
        }
        setStatus((s) => (s === 'live' || s === 'connecting' ? 'ended' : s))
        stopTimer()
      }

      // Open the microphone now so the permission prompt happens up front, but
      // attachMic only streams once setupComplete has arrived. A webview without
      // mediaDevices (missing permission plumbing) fails here loudly instead of
      // running a call the model can never hear.
      if (!navigator.mediaDevices?.getUserMedia) {
        throw new Error('Microphone access is not available in this window. Check the app permissions.')
      }
      const stream = await navigator.mediaDevices.getUserMedia({
        // Echo cancellation matters here: the model's own voice leaking back
        // into the mic reads as the user barging in.
        audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      })
      micStreamRef.current = stream
      await attachMic(stream)
      // Status stays 'connecting' and the clock stays at zero until setupComplete
      // arrives; handleServerFrame starts both.
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not start the voice call')
      setStatus('ended')
      void end()
    }
  }, [attachMic, connectBeep, flushAudio, handleServerFrame, send])

  const end = useCallback(() => {
    stopTimer()
    // A call that never connected (startRef still 0) has no duration to record.
    setLastSeconds(startRef.current ? Math.max(0, Math.floor((Date.now() - startRef.current) / 1000)) : 0)
    startRef.current = 0
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
    playingRef.current.clear()
    nextPlayTimeRef.current = 0
    setupDoneRef.current = false
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
    // Muting mid-sentence would leave the server waiting on an unfinished
    // utterance; audioStreamEnd tells it the audio stream paused so it flushes.
    if (pausedRef.current) send({ realtimeInput: { audioStreamEnd: true } })
  }, [send])

  useEffect(() => () => end(), [end])

  // formatElapsed renders seconds as MM:SS (or H:MM:SS past an hour).
  const display = useCallback(() => {
    const m = Math.floor(elapsed / 60)
    const s = elapsed % 60
    return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  }, [elapsed])

  return { status, elapsed, display, lastSeconds, error, paused, togglePause, start, end }
}
