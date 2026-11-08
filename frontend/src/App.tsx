import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { useQueryClient } from '@tanstack/react-query'
import { Time } from '../bindings/events'
import Toolbar from './components/layout/Toolbar'
import Sidebar from './components/layout/Sidebar'
import SettingsSidebar from './components/settings/SettingsSidebar'
const Onboarding = lazy(() => import('./pages/Onboarding'))
const SignIn = lazy(() => import('./pages/SignIn'))
const Home = lazy(() => import('./pages/Home'))
const Chat = lazy(() => import('./pages/Chat'))
const ArtifactViewer = lazy(() => import('./pages/ArtifactViewer'))
import { getChat } from './domains/chat/api'
import { usePairing } from './domains/account/hooks'
import Spinner from './components/Spinner'

// Blank black canvas.
//
// The Go -> frontend event wiring is kept alive even though nothing renders
// yet: the server clock is parked on the canvas as a `data-server-time`
// attribute, so it is readable in DevTools and ready for the first screen to
// consume without re-subscribing.
//
// Backend bindings are generated into ../bindings (refresh with
// `wails3 generate bindings`) and are untouched. Import one when a screen
// needs it, e.g.:
//     import { GreetService } from '../bindings/github.com/anyscapelabs/argus'

export default function App() {
  const canvasRef = useRef<HTMLDivElement | null>(null);
  const qc = useQueryClient()
  // The shell boots through pairing first: pending (checking the keyring),
  // absent → onboarding, invalid → sign-in, valid → the agent screens. The
  // later states emerge from this one source of truth, so every route read of
  // `pairing` stays decisive.
  const { pairing, leave } = usePairing(true)
  const [page, setPage] = useState<'onboarding' | 'signin' | 'home' | 'chat' | 'settings'>('onboarding');
  // Which document is open full screen, by id. Not a boolean: the chat card, the
  // panel list and the viewer all need to agree on *which* document the user
  // meant, and closing is naming none of them.
  const [openArtifact, setOpenArtifact] = useState<string | null>(null);
  const [sidebarMode, setSidebarMode] = useState<'main' | 'settings'>('main');
  const [settingsReturn, setSettingsReturn] = useState<'home' | 'chat'>('home');
  const [pendingSend, setPendingSend] = useState<{ id: number; text: string; modelRef: string | null } | null>(null);

  // Boot: hold the shell on the black canvas until the keyring check settles.
  // A pairing that exists but no longer proves (revoked key, or the backend
  // being unreachable) belongs on the sign-in screen — the account is still
  // half-paired there, the error is visible, and "Connect" retries; only a
  // truly unpaired keyring starts the onboarding tour.
  const [booted, setBooted] = useState(false);
  useEffect(() => {
    if (booted || pairing.status === 'pending') return
    setBooted(true)
    if (pairing.status === 'valid') setPage('home')
    else if (pairing.status === 'invalid') setPage('signin')
    else setPage('onboarding')
  }, [pairing.status, booted])

  // Protected routes: the agent screens exist only behind a verified key. Sign
  // out (or a revoked key discovered on entitlements refetch) lands here.
  useEffect(() => {
    if (!booted) return
    if (pairing.status === 'valid') return
    if (page === 'home' || page === 'chat' || page === 'settings') setPage('signin')
  }, [booted, pairing.status, page])

  // While the shell is authenticated and resting on home, an existing chat
  // picks up where it left off: the stream of history decides the landing.
  // Runs after boot — on mount it would race the pairing effect, which
  // rewrites the page as soon as the keyring check settles.
  useEffect(() => {
    if (!booted || pairing.status !== 'valid' || page !== 'home') return
    let cancelled = false
    getChat('default')
      .then((res) => {
        if (cancelled) return
        if (res?.messages && res.messages.length > 0) {
          setPage('chat')
        }
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [booted, pairing.status, page])

  useEffect(() => {
    const invalidateAll = () => {
      qc.invalidateQueries()
    }
    const unsubs = [
      Events.On('providers:changed', invalidateAll),
      Events.On('selections:changed', invalidateAll),
      Events.On('slots:changed', invalidateAll),
      Events.On('catalog:updated', invalidateAll),
    ]
    return () => unsubs.forEach((u) => u())
  }, [qc])


  useEffect(() => {
    // `Events.On` returns an unsubscribe function. StrictMode mounts effects
    // twice in development, so tearing down is what keeps the listener from
    // being registered twice.
    const unsubscribe = Events.On(Time, (event) => {
      canvasRef.current?.setAttribute('data-server-time', String(event.data));
    });
    return unsubscribe;
  }, []);

  // One viewer, opened by id, laid over whichever page named it. Both the landing
  // panel and the chat can reach a document, and neither should carry its own
  // copy of this overlay.
  const artifactOverlay = openArtifact ? (
    <div className="fixed inset-0 z-50">
      <Suspense fallback={null}>
        <ArtifactViewer artifactID={openArtifact} onBack={() => setOpenArtifact(null)} />
      </Suspense>
    </div>
  ) : null

  if (!booted) {
    return (
      <div className="flex min-h-dvh w-full items-center justify-center bg-primary">
        <Spinner />
      </div>
    )
  }

  return (
    <div
      ref={canvasRef}
      className="flex min-h-dvh w-full flex-col bg-primary text-grey-100"
    >
      <Suspense fallback={<div className="flex flex-1 items-center justify-center"><Spinner /></div>}>
        {page === 'onboarding' ? (
        <>
          <Toolbar />
          <Onboarding onContinue={() => setPage('signin')} />
        </>
      ) : page === 'signin' ? (
        <>
          <Toolbar />
          <SignIn onAuthenticated={() => setPage('home')} />
        </>
      ) : page === 'home' ? (
        <div className="flex flex-1 min-h-0">
          <Sidebar mode={sidebarMode} onToggleSettings={() => {
            if (sidebarMode === 'settings') {
              setSidebarMode('main')
            } else {
              setSidebarMode('settings')
              setSettingsReturn('home')
              setPage('settings')
            }
          }} onSignOut={() => { void leave.mutateAsync() }} />
          <Home onSend={(text: string, modelRef: string | null) => {
            setPendingSend({ id: Date.now(), text, modelRef });
            setPage('chat');
          }} onOpenArtifact={(id) => setOpenArtifact(id)} />
          {artifactOverlay}
        </div>
      ) : page === 'settings' ? (
        <div className="flex flex-1 min-h-0">
          <SettingsSidebar onBack={() => {
            setSidebarMode('main')
            setPage(settingsReturn)
          }} />
          <div className="flex flex-1 min-h-0 flex-col">
            <Toolbar />
          </div>
        </div>
      ) : (
        <div className="relative flex flex-1 min-h-0">
          <Sidebar mode={sidebarMode} onToggleSettings={() => {
            if (sidebarMode === 'settings') {
              setSidebarMode('main')
            } else {
              setSidebarMode('settings')
              setSettingsReturn('chat')
              setPage('settings')
            }
          }} onSignOut={() => { void leave.mutateAsync() }} />
          <Chat pendingSend={pendingSend} onPendingConsumed={() => setPendingSend(null)} onOpenArtifact={(id) => setOpenArtifact(id)} />
          {artifactOverlay}
        </div>
      )}
      </Suspense>
    </div>
  );
}
