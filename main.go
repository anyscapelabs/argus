package main

import (
	"embed"

	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	servicecatalog "github.com/anyscapelabs/argus/Services/Catalog"
	servicechat "github.com/anyscapelabs/argus/Services/Chat"
	servicemodels "github.com/anyscapelabs/argus/Services/Models"
	serviceprovider "github.com/anyscapelabs/argus/Services/Provider"
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
func main() {

	core := Shared.NewCore()
	providers := &serviceprovider.Service{Core: core}
	models := &servicemodels.Service{Core: core}
	catalog := &servicecatalog.Service{Core: core}
	chat := &servicechat.Service{Core: core}

	app := application.New(application.Options{
		Name:        "argus",
		Description: "Argus desktop app",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(providers),
			application.NewService(models),
			application.NewService(catalog),
			application.NewService(chat),
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
