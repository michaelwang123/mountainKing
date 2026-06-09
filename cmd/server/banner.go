// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package main

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/michaelwang123/mountainKing/internal/config"
	"github.com/michaelwang123/mountainKing/internal/datasource"
	"github.com/michaelwang123/mountainKing/internal/observability"
)

// printBanner outputs startup information. In development mode it prints a
// formatted banner to stdout for quick visual feedback. In production mode it
// emits a structured JSON log line via logger.Info.
func printBanner(cfg *config.Config, logger *observability.Logger, dsManager *datasource.DataSourceManager) {
	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	if cfg.Server.Mode == "development" {
		// Build datasource status lines.
		dsLines := make([]string, 0, len(cfg.Datasources))
		for _, ds := range cfg.Datasources {
			if !ds.Enabled {
				continue
			}
			status := dsManager.Status(ds.Name)
			mark := "✓"
			if status == nil || !status.Available {
				mark = "✗"
			}
			dsLines = append(dsLines, fmt.Sprintf("%s (%s) %s", ds.Name, ds.Type, mark))
		}

		// Determine auth display.
		authDisplay := cfg.Auth.Method
		if authDisplay == "" || authDisplay == "none" {
			authDisplay = "disabled"
		}

		fmt.Println()
		fmt.Printf("  MountainKing GraphQL API  %s\n", version)
		fmt.Println("  ⚠  DEVELOPMENT MODE — not for production")
		fmt.Println()
		fmt.Printf("  Listen:      http://localhost%s\n", addr)
		fmt.Printf("  Playground:  http://localhost%s/playground\n", addr)
		fmt.Printf("  Auth:        %s\n", authDisplay)
		for _, dsLine := range dsLines {
			fmt.Printf("  Datasources: %s\n", dsLine)
		}
		fmt.Println()
	} else {
		logger.Info("starting server",
			zap.String("addr", addr),
			zap.String("mode", cfg.Server.Mode),
			zap.String("version", version),
		)
	}
}
