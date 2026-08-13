package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"axiom/EventCollector/filewatcher"
	"axiom/EventCollector/shellhook"
	"axiom/EventCollector/windowscollector"
	"axiom/pkg/analytics"
	"axiom/pkg/api"
	"axiom/pkg/cli"
	"axiom/pkg/data"
	"axiom/pkg/goals"
)

func runHookClient(payload string) {
	conn, err := net.DialTimeout("pipe", shellhook.PipeName, 100*time.Millisecond)
	if err != nil {
		// Silently exit if daemon is not running
		os.Exit(0)
	}
	defer conn.Close()
	fmt.Fprintln(conn, payload)
	os.Exit(0)
}

func main() {
	// AXIOM Hook CLI support & Commands
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "hook":
			if len(os.Args) >= 3 {
				runHookClient(os.Args[2])
			}
			return
		case "today":
			cli.PrintTodayDashboard()
			return
		case "chat":
			cli.StartChat()
			return
		case "analyze":
			cli.GenerateFrictionReport()
			return
		}
	}

	log.Println("⚡ Initializing AXIOM Life OS Daemon...")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("❌ Failed to get user home directory: %v", err)
	}

	axiomDir := filepath.Join(homeDir, ".axiom")
	goalsPath := filepath.Join(axiomDir, "goals.yaml")
	dbPath := filepath.Join(axiomDir, "memory.db")

	// Ensure ~/.axiom/ directory exists
	if err := os.MkdirAll(axiomDir, 0755); err != nil {
		log.Fatalf("❌ Failed to create ~/.axiom directory: %v", err)
	}

	// Step 2: Load the goals configuration engine
	var goalsConfig *goals.Goals
	if _, err := os.Stat(goalsPath); os.IsNotExist(err) {
		log.Printf("⚠️ Configuration not found at %s. Running with default empty goals context.", goalsPath)
		goalsConfig = &goals.Goals{}
	} else {
		goalsConfig, err = goals.LoadGoals(goalsPath)
		if err != nil {
			log.Fatalf("❌ Error loading goals configuration: %v", err)
		}
		log.Printf("🎯 Loaded goals profile for: %s (%s)", goalsConfig.Identity.Name, goalsConfig.Identity.TargetRole)
	}

	// Step 3: Initialize SQLite Database
	sqlDB, err := data.InitDB(dbPath)
	if err != nil {
		log.Fatalf("❌ Database initialization failed: %v", err)
	}
	defer sqlDB.Close()
	log.Printf("💾 SQLite database initialized at: %s", dbPath)

	// Start the single database writer pipeline worker in background
	go data.StartDBWorker(sqlDB, goalsConfig)

	// Create cancellable context for clean shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start Phase 2 Analytics Aggregator
	go analytics.RollupWorker(ctx, sqlDB, goalsConfig)



	// ---------------------------------------------------------
	// SENSOR LAUNCH PAD
	// ---------------------------------------------------------

	// 0. API Server (Listens for Chrome Extension Webhooks)
	go func() {
		err := api.StartServer(4444, goalsConfig, data.DBChan)
		if err != nil {
			log.Printf("❌ Local API Server runtime error: %v", err)
		}
	}()

	// ---------------------------------------------------------
	// Spawn the Java Mascot UI automatically in the background
	// ---------------------------------------------------------
	log.Println("👾 Booting AXIOM Mascot Engine...")
	
	// Pre-flight cleanup: kill any orphaned Java mascots from previous runs
	log.Println("🧹 Sweeping for orphaned Mascot processes...")
	exec.Command("powershell", "-Command", "Stop-Process -Name java -Force -ErrorAction SilentlyContinue").Run()
	exec.Command("powershell", "-Command", "Stop-Process -Name javaw -Force -ErrorAction SilentlyContinue").Run()
	time.Sleep(500 * time.Millisecond)

	mascotCmd := exec.Command("java", "MascotOverlay")
	mascotCmd.Dir = "D:\\AXIOM\\mascot"
	mascotCmd.Stdout = os.Stdout // Pipe mascot logs to terminal
	mascotCmd.Stderr = os.Stderr
	
	err = mascotCmd.Start()
	if err != nil {
		log.Printf("⚠️ Warning: Could not start Java Mascot: %v\n", err)
	} else {
		log.Printf("👾 Mascot spawned successfully (PID: %d)", mascotCmd.Process.Pid)
	}

	// 1. File Watcher Sensor
	go func() {
		err := filewatcher.Run(ctx, filewatcher.Config{
			Goals:     goalsConfig,
			EventChan: data.DBChan,
		})
		if err != nil {
			log.Printf("❌ FileWatcher runtime error: %v", err)
		}
	}()

	// 2. Shell Hook Sensor (Named Pipe)
	go func() {
		err := shellhook.Run(ctx, shellhook.Config{
			Goals:     goalsConfig,
			EventChan: data.DBChan,
		})
		if err != nil {
			log.Printf("❌ ShellHook runtime error: %v", err)
		}
	}()

	// 3. Windows Foreground/Audio Collector Sensor
	// Note: Run this one in the main goroutine to block and keep the app alive
	err = windowscollector.Run(ctx, windowscollector.Config{
		PollInterval: 1 * time.Second,
		Goals:        goalsConfig,
		EventChan:    data.DBChan,
	})
	if err != nil {
		log.Fatalf("❌ Window collector runtime error: %v", err)
	}

	log.Println("🛑 Stop signal received. Initiating graceful shutdown...")
	if mascotCmd.Process != nil {
		log.Println("🛑 Terminating Mascot...")
		mascotCmd.Process.Kill()
	}

	// The context is cancelled by signal.NotifyContext above, which triggers
	// ctx.Done() in all sensor goroutines. We let the OS clean up channels 
	// rather than manually closing data.DBChan, to prevent race conditions 
	// where lingering goroutines might panic trying to write to a closed channel.
	time.Sleep(2 * time.Second)

	// Give the SQLite worker a moment to flush remaining cache queries to disk
	time.Sleep(1 * time.Second)
	log.Println("👋 AXIOM Daemon stopped cleanly.")
}
