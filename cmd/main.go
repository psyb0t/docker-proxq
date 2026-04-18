package main

import (
	"log/slog"
	"os"

	"github.com/psyb0t/proxq/internal/app"
	_ "github.com/psyb0t/slog-configurator"
)

func main() {
	if err := app.Run(); err != nil {
		slog.Error("run error", "error", err)
		os.Exit(1)
	}
}
