package cli

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"axiom/pkg/llm"
	_ "modernc.org/sqlite"
)

// StartChat launches the interactive AXIOM terminal REPL
func StartChat() {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".axiom", "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("❌ Failed to open AXIOM memory core: %v\n", err)
		return
	}
	defer db.Close()

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║               AXIOM OS — Intelligent Terminal                ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println("Connecting to Qwen2.5:3b (Thinking Mode)...")
	fmt.Println("\nAXIOM is listening. (Type 'exit' to quit)")

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		trigger := scanner.Text()
		if trigger == "exit" || trigger == "quit" {
			break
		}
		if trigger == "" {
			continue
		}

		// Refresh context dynamically so it's never stale
		dateStr := time.Now().Format("2006-01-02")
		var codingMin, entMin, totalActive, commits int
		var focusScore float64

		db.QueryRow(`
			SELECT coding_minutes, entertainment_minutes, total_active_minutes, commits, focus_score
			FROM daily_stats WHERE date = ?
		`, dateStr).Scan(&codingMin, &entMin, &totalActive, &commits, &focusScore)

		context := fmt.Sprintf(`You are AXIOM, an AI assistant running locally on the user's laptop.
You are brutal, specific, and hold the user accountable. 
If they ask for a roast, be mean, funny, and ruthless about their terrible metrics. Otherwise, act as a strict DevSecOps assistant.

TODAY'S ACTUAL METRICS:
- Coding Time: %dm
- Entertainment Time: %dm
- Total Active Time: %dm
- Commits Today: %d
- Focus Score: %.1f%%

Respond aggressively but logically. Use these metrics as proof.`, codingMin, entMin, totalActive, commits, focusScore)

		fmt.Println("🤔 AXIOM is thinking (TTL: 0)...")
		
		response, err := llm.AskAxiom(trigger, context, true)
		if err != nil {
			fmt.Printf("\n❌ AXIOM Error: %v\n", err)
			continue
		}

		fmt.Printf("\nAXIOM: %s\n", response)
	}
}
