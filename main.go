package main

import (
	"embed"

	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	notifications "github.com/wailsapp/wails/v3/pkg/services/notifications"

	serviceaccount "github.com/anyscapelabs/argus/Services/Account"
	serviceartifacts "github.com/anyscapelabs/argus/Services/Artifacts"
	servicebrowser "github.com/anyscapelabs/argus/Services/Browser"
	servicecatalog "github.com/anyscapelabs/argus/Services/Catalog"
	servicechat "github.com/anyscapelabs/argus/Services/Chat"
	servicemcp "github.com/anyscapelabs/argus/Services/MCP"
	servicemcpconnect "github.com/anyscapelabs/argus/Services/MCPConnect"
	servicemodels "github.com/anyscapelabs/argus/Services/Models"
	servicepermission "github.com/anyscapelabs/argus/Services/Permission"
	serviceprovider "github.com/anyscapelabs/argus/Services/Provider"
	servicescheduler "github.com/anyscapelabs/argus/Services/Scheduler"
	serviceuploads "github.com/anyscapelabs/argus/Services/Uploads"
	"github.com/anyscapelabs/argus/Services/Shared"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

//go:embed frontend/public/appicon.png
var appIcon []byte

func init() {
	// Register a custom event whose associated data type is string.
	// This is not required, but the binding generator will pick up registered events
	// and provide a strongly typed JS/TS API for them.
	application.RegisterEvent[string]("time")
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
// systemNotifier adapts Wails' notification service to the narrow sink the
// scheduler uses; keeping it behind an interface means the engine never
// depends on the notification library.
type systemNotifier struct {
	service *notifications.NotificationService
}

func (n systemNotifier) Notify(title, body string) {
	if err := n.service.SendNotification(notifications.NotificationOptions{
		Title: title,
		Body:  body,
	}); err != nil {
		log.Printf("notifications: could not deliver %q: %v", title, err)
	}
}

func main() {
	// Force the Linux build onto the X11 backend instead of Wayland. GTK/webkit
	// honour these before the first window is created, so set them up front.
	if os.Getenv("GDK_BACKEND") == "" {
		os.Setenv("GDK_BACKEND", "x11")
	}
	os.Setenv("XDG_SESSION_TYPE", "x11")
	os.Unsetenv("WAYLAND_DISPLAY")

	core := Shared.NewCore()
	account := &serviceaccount.Service{Core: core}
	providers := &serviceprovider.Service{Core: core}
	models := &servicemodels.Service{Core: core}
	catalog := &servicecatalog.Service{Core: core}
	permissions := &servicepermission.Service{Core: core}
	mcp := &servicemcp.Service{Core: core}
	mcpConnect := &servicemcpconnect.Service{Core: core, MCP: mcp}
	artifacts := &serviceartifacts.Service{Core: core}
	uploads := &serviceuploads.Service{Core: core}
	browser := &servicebrowser.Service{}
	chat := &servicechat.Service{Core: core, Permissions: permissions, MCP: mcp, MCPConnect: mcpConnect, Artifacts: artifacts, Browser: browser}
	// When a sign-in settles, the connector hands the turn back to the agent
	// instead of waiting for the user to say "I connected". Chat is set here
	// rather than constructed inside the connector because Chat imports
	// MCPConnect — a field like this is the only direction that compiles.
	mcpConnect.OnSettled = func(agentID, text string) {
		res := chat.Steer(agentID, text)
		if res.Error != nil {
			log.Printf("mcpconnect: steering run for %s refused: %s", agentID, res.Error.Message)
		}
	}
	notifier := notifications.New()
	scheduler := &servicescheduler.Service{Core: core, Chat: chat, Notifier: systemNotifier{service: notifier}}

	app := application.New(application.Options{
		Name:        "argus",
		Description: "Argus desktop app",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(account),
			application.NewService(providers),
			application.NewService(models),
			application.NewService(catalog),
			application.NewService(permissions),
			application.NewService(mcp),
			application.NewService(mcpConnect),
			application.NewService(artifacts),
			application.NewService(uploads),
			application.NewService(chat),
			application.NewService(notifier),
			application.NewService(scheduler),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	core.Notifier(func(name string, data any) {
		application.Get().Event.Emit(name, data)
	})
	core.PickerHook = models.Picker

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Argus",
		// Frameless drops the native title bar and window decorations on every
		// platform. On Linux this is `gtk_window_set_decorated(FALSE)`; the
		// runtime then takes over edge resizing itself (a 5px band at each edge,
		// active only while the `frameless` flag is set), so resize survives the
		// removal of the WM-drawn frame. Dragging the window uses the
		// `--wails-draggable: drag` property already set on <body>.
		Frameless: true,
		// Window sized to the golden ratio (1000 / 618 ≈ 1.618).
		Width:  1000,
		Height: 618,
		StartState: application.WindowStateMaximised,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		// Matches the frontend's black canvas (#000000) so the native window
		// underlay and the webview are the same colour during load and resize.
		BackgroundColour: application.NewRGB(0, 0, 0),
		URL:              "/",
	})

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
