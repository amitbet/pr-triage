//go:build !desktop

package main

import "context"

const desktopBuild = false

func runDesktop(ctx context.Context, o options) error { return runServe(ctx, o) }
