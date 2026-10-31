import { useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { useQueryClient } from '@tanstack/react-query'
import { Time } from '../bindings/events'
import Toolbar from './components/layout/Toolbar'
import Sidebar from './components/layout/Sidebar'
import SettingsSidebar from './components/settings/SettingsSidebar'
import Onboarding from './pages/Onboarding'
import Home from './pages/Home'
import Chat from './pages/Chat'
import { getChat } from './domains/chat/api'
import Providers from './pages/settings/Providers'

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
  const [page, setPage] = useState<'onboarding' | 'home' | 'chat' | 'settings'>('onboarding');
  const [sidebarMode, setSidebarMode] = useState<'main' | 'settings'>('main');
  const [pendingSend, setPendingSend] = useState<{ id: number; text: string; modelRef: string | null } | null>(null);

  useEffect(() => {
    getChat('default')
      .then((res) => {
        if (res?.messages && res.messages.length > 0) {
          setPage('chat')
        }
      })
      .catch(() => {})
  }, [])

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

  return (
    <div
      ref={canvasRef}
      className="flex min-h-dvh w-full flex-col bg-primary text-grey-100"
    >
      {page === 'onboarding' ? (
        <>
          <Toolbar />
          <Onboarding onContinue={() => setPage('home')} />
        </>
      ) : page === 'home' ? (
        <div className="flex flex-1 min-h-0">
          <Sidebar mode={sidebarMode} onToggleSettings={() => {
            if (sidebarMode === 'settings') {
              setSidebarMode('main')
            } else {
              setSidebarMode('settings')
              setPage('settings')
            }
          }} />
          <Home onSend={(text: string, modelRef: string | null) => {
            setPendingSend({ id: Date.now(), text, modelRef });
            setPage('chat');
          }} />
        </div>
      ) : page === 'settings' ? (
        <div className="flex flex-1 min-h-0">
          <SettingsSidebar onBack={() => {
            setSidebarMode('main')
            setPage('home')
          }} />
          <div className="flex flex-1 min-h-0 flex-col">
            <Toolbar />
            <Providers />
          </div>
        </div>
      ) : (
        <div className="flex flex-1 min-h-0">
          <Sidebar mode={sidebarMode} onToggleSettings={() => {
            if (sidebarMode === 'settings') {
              setSidebarMode('main')
            } else {
              setSidebarMode('settings')
              setPage('settings')
            }
          }} />
          <Chat pendingSend={pendingSend} onPendingConsumed={() => setPendingSend(null)} />
        </div>
      )}
    </div>
  );
}
