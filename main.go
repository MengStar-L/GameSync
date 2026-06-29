package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	windowsoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed resource/im1.png
var trayIconPNG []byte

func main() {
	app := NewApp()
	windowState := app.startupWindowState()

	err := wails.Run(&options.App{
		Title:             "GameSync",
		Width:             windowState.Width,
		Height:            windowState.Height,
		MinWidth:          1200,
		MinHeight:         780,
		StartHidden:       true,
		HideWindowOnClose: true,
		BackgroundColour:  &options.RGBA{R: 238, G: 242, B: 247, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:        app.startup,
		OnDomReady:       app.domReady,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		WindowStartState: windowState.WindowStartState(),
		Windows: &windowsoptions.Options{
			ResizeDebounceMS: 0,
			WindowClassName:  "GameSyncWindow",
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
