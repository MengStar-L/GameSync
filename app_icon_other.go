//go:build !windows

package main

import "context"

func (a *App) applyWindowIcon(ctx context.Context) {}

func (a *App) releaseWindowIcon() {}
