//go:build desktop

package main

import (
	"context"

	"github.com/wailsapp/wails/v2"
	wailsOptions "github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

const desktopBuild = true

func runDesktop(_ context.Context, o options) error {
	handler, err := newServeHandler(o)
	if err != nil {
		return err
	}
	return wails.Run(&wailsOptions.App{
		Title:       "PR Triage",
		Width:       1280,
		Height:      800,
		MinWidth:    900,
		MinHeight:   600,
		AssetServer: &assetserver.Options{Handler: handler},
	})
}
