import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { useQueryClient } from '@tanstack/react-query'
import { motion } from 'framer-motion'
import { FiWifiOff } from 'react-icons/fi'
import { Time } from '../bindings/events'
import Toolbar from './components/layout/Toolbar'
import Sidebar from './components/layout/Sidebar'
import { NewAgentCard } from './components/NewAgentCard'
import SettingsSidebar from './components/settings/SettingsSidebar'
import type { SettingsTab } from './components/settings/SettingsSidebar'
import Spinner from './components/Spinner'
import { usePairing, useAccount } from './domains/account/hooks'
import { useThemeMode, useAccent } from './lib/theme'

const Onboarding = lazy(() => import('./pages/Onboarding'))
const SignIn = lazy(() => import('./pages/SignIn'))
const Chat = lazy(() => import('./pages/Chat'))
const Usage = lazy(() => import('./pages/Usage'))
const Profile = lazy(() => import('./pages/Profile'))
const Appearance = lazy(() => import('./pages/Appearance'))
const Notifications = lazy(() => import('./pages/Notifications'))
const BrowserSettings = lazy(() => import('./pages/BrowserSettings'))
const Permissions = lazy(() => import('./pages/Permissions'))
const Billing = lazy(() => import('./pages/Billing'))
const Integrations = lazy(() => import('./pages/Integrations'))
const IntegrationDetail = lazy(() => import('./pages/IntegrationDetail'))
const ArtifactViewer = lazy(() => import('./pages/ArtifactViewer'))

type Page = 'signin' | 'onboarding' | 'chat' | 'settings' | 'integrations' | 'integration'

export default function App() {
  const canvasRef = useRef<HTMLDivElement | null>(null)
  const qc = useQueryClient()

  const { pairing, leave } = usePairing(true)
  const { data: account } = useAccount()
  const [page, setPage] = useState<Page>('signin')
  const [openArtifact, setOpenArtifact] = useState<string | null>(null)
  const [newAgentOpen, setNewAgentOpen] = useState(false)
  const [sidebarMode, setSidebarMode] = useState<'main' | 'settings'>('main')
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [activeAgent, setActiveAgent] = useState('default')
  const [settingsReturn, setSettingsReturn] = useState<'chat' | 'integrations'>('chat')
  const [integrationsReturn, setIntegrationsReturn] = useState<'chat'>('chat')
  const [activeIntegration, setActiveIntegration] = useState<string | null>(null)
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('usage')

  const [booted, setBooted] = useState(false)

  // On narrow windows the expanded sidebar would leave too little room for the
  // conversation, so it folds itself away; it only unfolds again once the
  // window is comfortably wide (hysteresis stops flapping at the threshold).
  useEffect(() => {
    const onResize = () => {
      const w = window.innerWidth
      setSidebarCollapsed((prev) => {
        if (w < 880) return true
        if (prev && w >= 1040) return false
        return prev
      })
    }
    window.addEventListener('resize', onResize)
    onResize()
    return () => window.removeEventListener('resize', onResize)
  }, [])

  const openAgent = (id: string) => {
    setActiveAgent(id)
    setPage('chat')
  }

  useEffect(() => {
    if (booted || pairing.status === 'pending') return
    setBooted(true)
    if (pairing.status === 'valid') {
      setPage('chat')
      return
    }
    if (pairing.status === 'invalid') {
      setPage('onboarding')
      return
    }
    setPage('signin')
  }, [pairing.status, booted])

  useEffect(() => {
    if (!booted || pairing.status === 'valid') return
    if (page === 'chat' || page === 'settings' || page === 'integrations' || page === 'integration') {
      setPage(pairing.status === 'invalid' ? 'onboarding' : 'signin')
    }
  }, [booted, pairing.status, page])

  // Warm the chat chunk while the operator is still pairing, so the swap from
  // onboarding to the conversation never stalls on a suspended page.
  useEffect(() => {
    if (page !== 'onboarding') return
    void import('./pages/Chat')
  }, [page])

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

  useThemeMode()
  useAccent()

  const artifactOverlay = openArtifact ? (
    <div className="fixed inset-0 z-50">
      <Suspense fallback={null}>
        <ArtifactViewer artifactID={openArtifact} onBack={() => setOpenArtifact(null)} />
      </Suspense>
    </div>
  ) : null

  const newAgentOverlay = newAgentOpen ? (
    <div
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/50 p-4"
      onMouseDown={() => setNewAgentOpen(false)}
    >
      <div role="presentation" onMouseDown={(e) => e.stopPropagation()}>
        <NewAgentCard
          onClose={() => setNewAgentOpen(false)}
          onCreated={(agentID) => {
            setNewAgentOpen(false)
            openAgent(agentID)
          }}
        />
      </div>
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
    <div ref={canvasRef} className="flex h-dvh w-full flex-col bg-primary text-grey-100">
      {account?.offline && (
        <div
          role="status"
          className="flex shrink-0 items-center justify-center gap-2 bg-warning/15 px-3 py-1 text-xs text-warning"
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
        <motion.div
          key={page}
          initial={{ opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.18, ease: 'easeOut' }}
          className="flex min-h-0 flex-1 flex-col"
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
                onAuthenticated={() => setPage('chat')}
              />
            </>
          ) : page === 'integrations' ? (
            <div className="flex min-h-0 flex-1">
              <Sidebar
                mode={sidebarMode}
                collapsed={sidebarCollapsed}
                onCollapsedChange={setSidebarCollapsed}
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
                activeAgent={activeAgent}
                onSelectAgent={openAgent}
                onOpenNewAgent={() => setNewAgentOpen(true)}
                onSignOut={() => {
                  void leave.mutateAsync()
                }}
              />
              <Integrations
                onManage={(id) => {
                  setActiveIntegration(id)
                  setPage('integration')
                }}
              />
            </div>
          ) : page === 'integration' ? (
            <div className="flex min-h-0 flex-1">
              <Sidebar
                mode={sidebarMode}
                collapsed={sidebarCollapsed}
                onCollapsedChange={setSidebarCollapsed}
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
                  setPage('integrations')
                  setSidebarMode('main')
                }}
                onOpenChat={() => setPage('chat')}
                agentActive={false}
                activeAgent={activeAgent}
                onSelectAgent={openAgent}
                onOpenNewAgent={() => setNewAgentOpen(true)}
                onSignOut={() => {
                  void leave.mutateAsync()
                }}
              />
              <IntegrationDetail
                integrationId={activeIntegration ?? ''}
                onBack={() => {
                  setPage('integrations')
                  setSidebarMode('main')
                }}
              />
            </div>
          ) : page === 'settings' ? (
            <div className="flex min-h-0 flex-1">
              <SettingsSidebar
                activeTab={settingsTab}
                onSelectTab={setSettingsTab}
                onBack={() => {
                  setSidebarMode('main')
                  setPage(settingsReturn)
                }}
              />
              <div className="flex min-h-0 flex-1 flex-col">
                <Toolbar />
                {settingsTab === 'usage' && <Usage />}
                {settingsTab === 'profile' && <Profile onManageBilling={() => setSettingsTab('billing')} />}
                {settingsTab === 'appearance' && <Appearance />}
                {settingsTab === 'notifications' && <Notifications />}
                {settingsTab === 'browser' && <BrowserSettings />}
                {settingsTab === 'permissions' && <Permissions />}
                {settingsTab === 'billing' && <Billing />}
              </div>
            </div>
          ) : (
            <div className="relative flex min-h-0 flex-1">
              <Sidebar
                mode={sidebarMode}
                collapsed={sidebarCollapsed}
                onCollapsedChange={setSidebarCollapsed}
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
                  setIntegrationsReturn('chat')
                  setPage('integrations')
                  setSidebarMode('main')
                }}
                onOpenChat={() => setPage('chat')}
                agentActive={true}
                activeAgent={activeAgent}
                onSelectAgent={openAgent}
                onOpenNewAgent={() => setNewAgentOpen(true)}
                onSignOut={() => {
                  void leave.mutateAsync()
                }}
              />
              <Chat
                key={activeAgent}
                agentID={activeAgent}
                onOpenArtifact={(id) => setOpenArtifact(id)}
              />
              {artifactOverlay}
            </div>
          )}
        </motion.div>
      </Suspense>
      {newAgentOverlay}
    </div>
  )
}
