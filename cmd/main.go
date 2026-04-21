package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/psyb0t/docker-proxq/internal/app"
	_ "github.com/psyb0t/slog-configurator"
)

const defaultConfigPath = "config.yaml"

func main() {
	configPath := flag.String(
		"config", "", "path to config file",
	)

	flag.Parse()

	path := resolveConfigPath(*configPath)

	if err := app.Run(path); err != nil {
		slog.Error("run error", "error", err)
		os.Exit(1)
	}
}

func resolveConfigPath(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}

	if envPath := os.Getenv("PROXQ_CONFIG"); envPath != "" {
		return envPath
	}

	return defaultConfigPath
}
