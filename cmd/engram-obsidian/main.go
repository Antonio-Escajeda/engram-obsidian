package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Gentleman-Programming/engram-obsidian/internal/obsidian"
	"github.com/Gentleman-Programming/engram-obsidian/internal/obsidian/daemon"
)

const version = "0.1.0"

func main() {
	var (
		flagVersion       = flag.Bool("version", false, "Mostrar versión y salir")
		flagSelect        = flag.Bool("select", false, "Forzar apertura de la TUI para cambiar la selección")
		flagDaemon        = flag.Bool("daemon", false, "Modo daemon: sin TUI, usa selección guardada")
		flagInterval      = flag.Duration("interval", 10*time.Minute, "Intervalo de re-sync en background (ej: 5m, 1h)")
		flagPoll          = flag.Duration("poll", 2500*time.Millisecond, "Intervalo de polling para detección de Obsidian")
		flagSelectionFile = flag.String("selection", "", "Path del archivo de selección (default: ~/.engram/obsidian-selection.json)")
	)
	flag.Parse()

	if *flagVersion {
		fmt.Printf("engram-obsidian v%s\n", version)
		os.Exit(0)
	}

	selPath := *flagSelectionFile
	if selPath == "" {
		selPath = obsidian.DefaultSelectionPath()
	}

	cfg := daemon.DefaultConfig()
	cfg.SelectionPath = selPath
	cfg.SyncInterval = *flagInterval
	cfg.PollInterval = *flagPoll
	cfg.Logf = log.Printf

	cfg.ForceSelect = *flagSelect
	cfg.DaemonMode = *flagDaemon

	d := daemon.New(cfg)

	if *flagSelect {
		// Modo one-shot: abre TUI, sincroniza, sale. Sin loop, sin cleanup.
		if err := d.RunOnce(); err != nil {
			log.Printf("select error: %v", err)
			os.Exit(1)
		}
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := d.Run(ctx); err != nil && err != context.Canceled {
		log.Printf("daemon error: %v", err)
		os.Exit(1)
	}
}
