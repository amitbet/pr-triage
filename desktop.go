//go:build desktop

package main

import (
	"context"
	_ "embed"

	"github.com/wailsapp/wails/v2"
	wailsOptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

const desktopBuild = true

// Linux takes the window icon from here. macOS and Windows read it from the
// app bundle and the exe resources that scripts/build-desktop.sh adds.
//
//go:embed assets/icon/icon.png
var desktopIcon []byte

func runDesktop(_ context.Context, o options) error {
	inheritShellPath()
	handler, err := newServeHandler(o)
	if err != nil {
		return err
	}
	return wails.Run(&wailsOptions.App{
		Title:       "PR Manager",
		Width:       1280,
		Height:      800,
		MinWidth:    900,
		MinHeight:   600,
		AssetServer: &assetserver.Options{Handler: handler},
		Linux:       &linux.Options{Icon: desktopIcon},
	})
}
