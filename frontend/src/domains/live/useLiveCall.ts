import { useCallback, useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { endLiveSession, getLiveConfig, runLiveTool } from './api'
import type { VoiceTurn } from './api'

export type VoiceStatus = 'idle' | 'connecting' | 'live' | 'ended'

// VoiceCallSummary is what a finished call leaves behind. `unanswered` is true
// only when the user spoke and the live model never produced any response.
export type VoiceCallSummary = {
  seconds: number
  unanswered: boolean
  turns: VoiceTurn[]
}

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
}

export function useLiveCall({ agentID = 'default' }: UseLiveCallOptions = {}) {
  const [status, setStatus] = useState<VoiceStatus>('idle')
  const [elapsed, setElapsed] = useState(0)
  const [error, setError] = useState<string | null>(null)
  // paused mutes the microphone without tearing the call down: no frames leave
  // while paused, and the model simply hears silence until it is lifted.
  const [paused, setPaused] = useState(false)
  // lastCall describes the most recently finished call (null until one ends
  // with something said), so the caller can persist it to the transcript.
  const [lastCall, setLastCall] = useState<VoiceCallSummary | null>(null)

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
  const sessionRef = useRef('')
  // frameCountRef caps the one-time server-frame diagnostics to the first few.
  const frameCountRef = useRef(0)
  // Transcript accumulation. inputBufRef/modelBufRef hold the utterance being
  // transcribed (Google streams the text in fragments); turnsRef holds every
  // completed side so far.
  const inputBufRef = useRef('')
  const modelBufRef = useRef('')
  const turnsRef = useRef<VoiceTurn[]>([])

  const stopTimer = () => {
    if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
  }

  // flushTurns closes the utterances in flight: whatever the transcription
  // fragments built up becomes a turn, and the buffers start empty. A side
  // with no text produces no turn.
  const flushTurns = useCallback(() => {
    const said = inputBufRef.current.trim()
    const answered = modelBufRef.current.trim()
    if (said) turnsRef.current.push({ role: 'user', text: said })
    if (answered) turnsRef.current.push({ role: 'model', text: answered })
    inputBufRef.current = ''
    modelBufRef.current = ''
  }, [])

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

  // handleFunctionCall is transport only: every function, catalog lookup,
  // permission check and side effect is owned by the backend session.
  //
  // Wire contract (Gemini Live): the model asks for tools in a TOP-LEVEL
  // `toolCall.functionCalls` frame — never inside serverContent.modelTurn — and
  // the client answers with a `toolResponse` frame whose entries carry the call's
  // `id` AND its `name`. Anything else leaves the model waiting for a result that
  // never arrives, which from the outside looks like a call that went silent.
  const handleFunctionCall = useCallback(
    async (call: { id: string; name: string; args?: Record<string, unknown> }) => {
      let output = ''
      let isError = false
      try {
        if (!sessionRef.current) throw new Error('The live tool session has ended')
        const result = await runLiveTool(sessionRef.current, call.id, call.name, call.args ?? {})
        output = result.output
        isError = result.isError
      } catch (err) {
        output = `tool call failed: ${(err as Error).message}`
        isError = true
      }
      send({
        toolResponse: {
          functionResponses: [
            { id: call.id, name: call.name, response: { output, is_error: isError, scheduling: 'INTERRUPT' } },
          ],
        },
      })
    },
    [send],
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
      // Transcription fragments. Google streams the words as they are spoken;
      // they only become turns at a turn boundary.
      if (typeof sc?.inputTranscription?.text === 'string') {
        inputBufRef.current += sc.inputTranscription.text
      }
      if (typeof sc?.outputTranscription?.text === 'string') {
        modelBufRef.current += sc.outputTranscription.text
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
        flushTurns()
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
    setLastCall(null)
    setPaused(false)
    pausedRef.current = false
    setupDoneRef.current = false
    startRef.current = 0
    stopTimer()
    frameCountRef.current = 0
    inputBufRef.current = ''
    modelBufRef.current = ''
    turnsRef.current = []
    flushAudio()
    setStatus('connecting')
    try {
      if (sessionRef.current) {
        await endLiveSession(sessionRef.current)
        sessionRef.current = ''
      }
      // Prompt, profile context and the compact gateway declarations are
      // rebuilt by Go for every call; the frontend owns only transport.
      const cfg = await getLiveConfig(agentID)
      sessionRef.current = cfg.session_id
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
            // Both sides of the call come back as text. This is what the
            // transcript card is built from; it adds no separate fee, only a
            // trivial number of text tokens on top of the audio already sent.
            inputAudioTranscription: {},
            outputAudioTranscription: {},
            systemInstruction: { parts: [{ text: cfg.system_instruction }] },
            tools: [{ functionDeclarations: cfg.tools ?? [] }],
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
        // The server ended the call: finalize the transcript and tear the rest
        // of the session down exactly as a user-initiated stop would.
        end()
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
  }, [agentID, attachMic, connectBeep, flushAudio, handleServerFrame, send])

  const end = useCallback(() => {
    stopTimer()
    if (sessionRef.current) {
      void endLiveSession(sessionRef.current)
      sessionRef.current = ''
    }
    // Close the utterance in flight before deciding whether the call was ever
    // answered. A partial or complete live-model response still counts as an
    // answer and belongs in the voice transcript card.
    flushTurns()
    const seconds = startRef.current ? Math.max(0, Math.floor((Date.now() - startRef.current) / 1000)) : 0
    // A call that never connected (startRef still 0) has no duration to record.
    if (turnsRef.current.length > 0) {
      const turns = turnsRef.current.slice()
      const unanswered = turns.some((turn) => turn.role === 'user') && !turns.some((turn) => turn.role === 'model')
      setLastCall({ seconds, unanswered, turns })
    } else {
      setLastCall(null)
    }
    startRef.current = 0
    turnsRef.current = []
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
  }, [flushTurns])

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
  useEffect(() => {
    const unsubscribe = Events.On('app:backgrounded', () => end())
    return () => unsubscribe?.()
  }, [end])

  // formatElapsed renders seconds as MM:SS (or H:MM:SS past an hour).
  const display = useCallback(() => {
    const m = Math.floor(elapsed / 60)
    const s = elapsed % 60
    return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  }, [elapsed])

  return { status, elapsed, display, lastCall, error, paused, togglePause, start, end }
}
