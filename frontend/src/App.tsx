import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { useQueryClient } from '@tanstack/react-query'
import { FiWifiOff } from 'react-icons/fi'
import { Time } from '../bindings/events'
import Toolbar from './components/layout/Toolbar'
import Sidebar from './components/layout/Sidebar'
import SettingsSidebar from './components/settings/SettingsSidebar'
import Spinner from './components/Spinner'
import { getChat } from './domains/chat/api'
import { usePairing, useAccount } from './domains/account/hooks'

const Onboarding = lazy(() => import('./pages/Onboarding'))
const SignIn = lazy(() => import('./pages/SignIn'))
const Home = lazy(() => import('./pages/Home'))
const Chat = lazy(() => import('./pages/Chat'))
const Integrations = lazy(() => import('./pages/Integrations'))
const ArtifactViewer = lazy(() => import('./pages/ArtifactViewer'))

type Page = 'signin' | 'onboarding' | 'home' | 'chat' | 'settings' | 'integrations'

export default function App() {
  const canvasRef = useRef<HTMLDivElement | null>(null)
  const qc = useQueryClient()

  const { pairing, leave } = usePairing(true)
  const { data: account } = useAccount()
  const [page, setPage] = useState<Page>('signin')
  const [openArtifact, setOpenArtifact] = useState<string | null>(null)
  const [sidebarMode, setSidebarMode] = useState<'main' | 'settings'>('main')
  const [settingsReturn, setSettingsReturn] = useState<'home' | 'chat'>('home')
  const [integrationsReturn, setIntegrationsReturn] = useState<'home' | 'chat'>('chat')
  const [pendingSend, setPendingSend] = useState<{
    id: number
    text: string
    modelRef: string | null
    attachments: { id: string; filename: string; kind: string; dataUrl?: string }[]
  } | null>(null)

  const [booted, setBooted] = useState(false)

  // Settle initial route once keyring check completes.
  useEffect(() => {
    if (booted || pairing.status === 'pending') return
    setBooted(true)
    if (pairing.status === 'valid') {
      setPage('home')
      return
    }
    if (pairing.status === 'invalid') {
      setPage('onboarding')
      return
    }
    setPage('signin')
  }, [pairing.status, booted])

  // Guard authenticated screens if key is signed out or revoked.
  useEffect(() => {
    if (!booted || pairing.status === 'valid') return
    if (page === 'home' || page === 'chat' || page === 'settings' || page === 'integrations') {
      setPage(pairing.status === 'invalid' ? 'onboarding' : 'signin')
    }
  }, [booted, pairing.status, page])

  // Resume existing default conversation on home landing.
  useEffect(() => {
    if (!booted || pairing.status !== 'valid' || page !== 'home') return
    let cancelled = false
    getChat('default')
      .then((res) => {
        if (cancelled) return
        if (res?.messages && res.messages.length > 0) setPage('chat')
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [booted, pairing.status, page])

  useEffect(() => {
    const invalidateAll = () => qc.invalidateQueries()
    const unsubs = [
      Events.On('providers:changed', invalidateAll),
      Events.On('selections:changed', invalidateAll),
      Events.On('slots:changed', invalidateAll),
      Events.On('catalog:updated', invalidateAll),
    ]
    return () => unsubs.forEach((u) => u())
  }, [qc])

  useEffect(() => {
    const unsubscribe = Events.On(Time, (event) => {
      canvasRef.current?.setAttribute('data-server-time', String(event.data))
    })
    return unsubscribe
  }, [])

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
    <div ref={canvasRef} className="flex min-h-dvh w-full flex-col bg-primary text-grey-100">
      {account?.offline && (
        <div
          role="status"
          className="flex shrink-0 items-center justify-center gap-2 bg-yellow-500/15 px-3 py-1 text-xs text-yellow-200"
        >
          <FiWifiOff size={12} aria-hidden="true" />
          Offline — showing your last synced plan and identity. Changes sync when the backend is reachable.
        </div>
      )}

      <Suspense
        fallback={
          <div className="flex flex-1 items-center justify-center">
            <Spinner />
          </div>
        }
      >
        {page === 'signin' ? (
          <>
            <Toolbar />
            <SignIn
              onSignIn={() => setPage('onboarding')}
              onCreateAccount={() => setPage('onboarding')}
            />
          </>
        ) : page === 'onboarding' ? (
          <>
            <Toolbar />
            <Onboarding
              onBack={() => setPage('signin')}
              onAuthenticated={() => setPage('home')}
            />
          </>
        ) : page === 'home' ? (
          <div className="flex min-h-0 flex-1">
            <Sidebar
              mode={sidebarMode}
              onToggleSettings={() => {
                if (sidebarMode === 'settings') {
                  setSidebarMode('main')
                  return
                }
                setSidebarMode('settings')
                setSettingsReturn('home')
                setPage('settings')
              }}
              onOpenIntegrations={() => {
                setIntegrationsReturn('home')
                setPage('integrations')
                setSidebarMode('main')
              }}
              onOpenChat={() => setPage('chat')}
              agentActive={false}
              onSignOut={() => {
                void leave.mutateAsync()
              }}
            />
            <Home
              onSend={(text, modelRef, attachments) => {
                setPendingSend({ id: Date.now(), text, modelRef, attachments: attachments ?? [] })
                setPage('chat')
              }}
              onOpenArtifact={(id) => setOpenArtifact(id)}
            />
            {artifactOverlay}
          </div>
        ) : page === 'integrations' ? (
          <div className="flex min-h-0 flex-1">
            <Sidebar
              mode={sidebarMode}
              onToggleSettings={() => {
                if (sidebarMode === 'settings') {
                  setSidebarMode('main')
                  return
                }
                setSidebarMode('settings')
                setSettingsReturn(integrationsReturn)
                setPage('settings')
              }}
              onOpenIntegrations={() => {
                setIntegrationsReturn('chat')
                setPage('integrations')
                setSidebarMode('main')
              }}
              onOpenChat={() => setPage('chat')}
              agentActive={false}
              onSignOut={() => {
                void leave.mutateAsync()
              }}
            />
            <Integrations />
          </div>
        ) : page === 'settings' ? (
          <div className="flex min-h-0 flex-1">
            <SettingsSidebar
              onBack={() => {
                setSidebarMode('main')
                setPage(settingsReturn)
              }}
            />
            <div className="flex min-h-0 flex-1 flex-col">
              <Toolbar />
            </div>
          </div>
        ) : (
          <div className="relative flex min-h-0 flex-1">
            <Sidebar
              mode={sidebarMode}
              onToggleSettings={() => {
                if (sidebarMode === 'settings') {
                  setSidebarMode('main')
                  return
                }
                setSidebarMode('settings')
                setSettingsReturn('chat')
                setPage('settings')
              }}
              onOpenIntegrations={() => {
                setIntegrationsReturn(page === 'chat' || page === 'integrations' ? 'chat' : 'home')
                setPage('integrations')
                setSidebarMode('main')
              }}
              onOpenChat={() => setPage('chat')}
              agentActive={true}
              onSignOut={() => {
                void leave.mutateAsync()
              }}
            />
            <Chat
              pendingSend={pendingSend}
              onPendingConsumed={() => setPendingSend(null)}
              onOpenArtifact={(id) => setOpenArtifact(id)}
            />
            {artifactOverlay}
          </div>
        )}
      </Suspense>
    </div>
  )
}
