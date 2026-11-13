package main

import (
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/anyscapelabs/argus/Services/Shared"
)

// backgroundLifecycle keeps the process and its existing webview alive when
// the window closes. Hiding does not reload React or cancel Go contexts, so an
// active agent run, reminders and routines continue without another token.
type backgroundLifecycle struct {
	app        *application.App
	window     *application.WebviewWindow
	tray       *application.SystemTray
	statusItem *application.MenuItem
	quitting   atomic.Bool
	activeRuns atomic.Int64
}

func newBackgroundLifecycle(app *application.App, window *application.WebviewWindow, icon []byte) *backgroundLifecycle {
	lifecycle := &backgroundLifecycle{app: app, window: window}
	tray := app.SystemTray.New()
	tray.SetIcon(icon)
	tray.SetTooltip("Argus is ready in the background")
	lifecycle.tray = tray

	menu := app.NewMenu()
	lifecycle.statusItem = menu.Add(backgroundStatus(0)).SetEnabled(false)
	menu.Add("Open Argus").OnClick(func(*application.Context) {
		lifecycle.show()
	})
	menu.AddSeparator()
	menu.Add("Quit Argus").OnClick(func(*application.Context) {
		lifecycle.quitting.Store(true)
		app.Quit()
	})
	tray.SetMenu(menu)
	tray.OnClick(lifecycle.show)

	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if lifecycle.quitting.Load() {
			return
		}
		window.Hide()
		app.Event.Emit("app:backgrounded", nil)
		event.Cancel()
		tray.SetTooltip("Argus is ready in the background")
	})

	// Clicking the macOS Dock icon should restore a window hidden by its close
	// button, just like selecting Open Argus from the tray menu.
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		lifecycle.show()
	})
	return lifecycle
}

func (l *backgroundLifecycle) show() {
	l.window.Show().Focus()
}

func (l *backgroundLifecycle) handleEvent(name string) {
	switch name {
	case Shared.EventChatRunStarted:
		l.activeRuns.Add(1)
	case Shared.EventChatRunFinished:
		for {
			current := l.activeRuns.Load()
			if current == 0 || l.activeRuns.CompareAndSwap(current, current-1) {
				break
			}
		}
	default:
		return
	}
	active := l.activeRuns.Load()
	l.statusItem.SetLabel(backgroundStatus(active))
	if active > 0 {
		l.tray.SetTooltip("Argus is working in the background")
	} else {
		l.tray.SetTooltip("Argus is ready in the background")
	}
}

func backgroundStatus(active int64) string {
	if active > 0 {
		return "Status: Working"
	}
	return "Status: Ready"
}
