package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/options"
)

const (
	windowStateNormal     = "normal"
	windowStateMaximised  = "maximised"
	windowStateFullscreen = "fullscreen"
)

type windowState struct {
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	StartState string `json:"startState"`
}

func defaultWindowState() windowState {
	return windowState{
		Width:      1500,
		Height:     960,
		StartState: windowStateNormal,
	}
}

func (s windowState) normalize() windowState {
	if s.Width < 1200 {
		s.Width = 1200
	}
	if s.Height < 780 {
		s.Height = 780
	}

	switch s.StartState {
	case windowStateNormal, windowStateMaximised, windowStateFullscreen:
	default:
		s.StartState = windowStateNormal
	}

	return s
}

func (s windowState) WindowStartState() options.WindowStartState {
	switch s.StartState {
	case windowStateMaximised:
		return options.Maximised
	case windowStateFullscreen:
		return options.Fullscreen
	default:
		return options.Normal
	}
}

func (s windowState) HasPosition() bool {
	return s.X != 0 || s.Y != 0
}

func loadWindowState(baseDir string) (windowState, error) {
	path := filepath.Join(baseDir, "window-state.json")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultWindowState(), nil
		}
		return defaultWindowState(), err
	}

	state := defaultWindowState()
	if err := json.Unmarshal(content, &state); err != nil {
		return defaultWindowState(), err
	}
	return state.normalize(), nil
}

func saveWindowState(baseDir string, state windowState) error {
	content, err := json.MarshalIndent(state.normalize(), "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(baseDir, "window-state.json")
	return os.WriteFile(path, content, 0o644)
}
