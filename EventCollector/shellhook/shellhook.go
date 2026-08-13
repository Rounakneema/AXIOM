package shellhook

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net"
	"strings"
	"time"

	"axiom/pkg/event"
	"axiom/pkg/goals"

	"github.com/Microsoft/go-winio"
)

const PipeName = `\\.\pipe\axiom-telemetry`

type Config struct {
	Goals     *goals.Goals
	EventChan chan<- event.Event
}

type ShellPayload struct {
	Command  string  `json:"command"`
	ExitCode int     `json:"exit_code"`
	Cwd      string  `json:"cwd"`
	Duration float64 `json:"duration"` // in seconds
	Shell    string  `json:"shell"`    // "powershell" or "cmd"
}

func Run(ctx context.Context, cfg Config) error {
	log.Println("🐚 Starting ShellHook sensor (Named Pipe)...")

	listener, err := winio.ListenPipe(PipeName, nil)
	if err != nil {
		return err
	}
	defer listener.Close()

	// Handle context cancellation to unblock Accept
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				log.Println("🛑 ShellHook sensor stopping...")
				return nil
			}
			log.Printf("⚠️ ShellHook accept error: %v", err)
			continue
		}

		go handleConnection(conn, cfg)
	}
}

func handleConnection(conn net.Conn, cfg Config) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Text()
		var payload ShellPayload
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			log.Printf("⚠️ ShellHook failed to parse JSON: %v", err)
			continue
		}

		// Filter out empty or trivial commands
		cmd := strings.TrimSpace(payload.Command)
		if cmd == "" {
			continue
		}

		// Visible logging for the user to confirm it works
		log.Printf("💻 Shell command recorded: %s (Exit Code: %d, Duration: %.2fs)", cmd, payload.ExitCode, payload.Duration)

		project, isRelevant := "", 0
		if cfg.Goals != nil {
			projName, matched := cfg.Goals.GetProjectForPath(payload.Cwd)
			if matched {
				project = projName
				isRelevant = 1
			}
		}

		e := event.Event{
			Type:         "terminal_command",
			Source:       "shellhook",
			Time:         time.Now(),
			Duration:     time.Duration(payload.Duration * float64(time.Second)),
			Title:        cmd,
			Category:     "coding",
			Project:      project,
			GoalRelevant: isRelevant,
			ExitCode:     payload.ExitCode,
			Metadata: map[string]string{
				"cwd":   payload.Cwd,
				"shell": payload.Shell,
			},
		}

		select {
		case cfg.EventChan <- e:
		default:
			log.Println("⚠️ ShellHook: Event pipeline is full, dropping event")
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("⚠️ ShellHook connection read error: %v", err)
	}
}
