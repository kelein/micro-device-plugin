package main

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kelein/micro-device-plugin/pkg/server"
)

func init() {
	initLogger()
}

func initLogger() {
	replace := func(groups []string, a slog.Attr) slog.Attr {
		// Remove the directory from the source's filename.
		if a.Key == slog.SourceKey {
			source := a.Value.Any().(*slog.Source)
			source.File = filepath.Base(source.File)
		}
		return a
	}

	// * JSON Log Format
	// logger := slog.New(slog.NewJSONHandler(
	// 	os.Stdout, &slog.HandlerOptions{AddSource: true},
	// ))

	// * Text Log Format
	logger := slog.New(slog.NewTextHandler(
		os.Stdout, &slog.HandlerOptions{
			AddSource:   true,
			ReplaceAttr: replace,
		},
	))

	slog.SetDefault(logger)
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
