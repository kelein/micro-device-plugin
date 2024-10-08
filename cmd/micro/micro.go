package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kelein/micro-device-plugin/pkg/server"
)

type logFmt uint32

// Log Output Format
const (
	JSON logFmt = iota
	TEXT
)

func init() {
	initLogger(TEXT)
}

func initLogger(f logFmt) {
	replace := func(groups []string, a slog.Attr) slog.Attr {
		// Use short source filename
		if a.Key == slog.SourceKey {
			source := a.Value.Any().(*slog.Source)
			source.File = filepath.Base(source.File)
		}
		return a
	}

	var h slog.Handler
	opts := slog.HandlerOptions{
		AddSource:   true,
		ReplaceAttr: replace,
	}
	switch f {
	case JSON:
		h = slog.NewJSONHandler(os.Stdout, &opts)
	case TEXT:
		h = slog.NewTextHandler(os.Stdout, &opts)
	}
	slog.SetDefault(slog.New(h))
}

func main() {
	slog.Info("staring micro device plugin ...")
	micro := server.NewMicroDeviceServer()
	go micro.Run()

	if err := micro.RegisterToKubelet(); err != nil {
		slog.Error("micro device plugin register failed", "err", err)
		os.Exit(1)
		return
	}
	slog.Error("micro device plugin register successfully")
}
