import { useEffect, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { Time } from '../bindings/events'
import Toolbar from './components/layout/Toolbar'
import Sidebar from './components/layout/Sidebar'
import Onboarding from './pages/Onboarding'
import Home from './pages/Home'

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
  const [page, setPage] = useState<'onboarding' | 'home'>('onboarding');

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
      ) : (
        <div className="flex flex-1 min-h-0">
          <Sidebar />
          <div className="flex flex-1 flex-col min-h-0">
            <Toolbar />
            <Home />
          </div>
        </div>
      )}
    </div>
  );
}
