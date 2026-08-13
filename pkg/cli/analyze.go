package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"axiom/pkg/llm"
	_ "modernc.org/sqlite"
)

// GenerateFrictionReport runs the Friction-Mining LLM analysis
func GenerateFrictionReport() {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".axiom", "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("❌ Failed to open AXIOM memory core: %v\n", err)
		return
	}
	defer db.Close()

	fmt.Println("🔍 AXIOM is analyzing your terminal friction and errors...")

	// Extract failed commands from the last 7 days
	sevenDaysAgo := time.Now().AddDate(0, 0, -7).UnixMilli()
	rows, err := db.Query(`
		SELECT title, exit_code, timestamp
		FROM events
		WHERE event_type = 'terminal_command' AND exit_code != 0
		AND timestamp >= ?
	`, sevenDaysAgo)
	
	if err != nil {
		fmt.Printf("❌ Failed to query events: %v\n", err)
		return
	}
	defer rows.Close()

	failedCommands := ""
	for rows.Next() {
		var title string
		var exitCode int
		var timestampMs int64
		if err := rows.Scan(&title, &exitCode, &timestampMs); err == nil {
			timestamp := time.UnixMilli(timestampMs)
			failedCommands += fmt.Sprintf("- [%s] Command: '%s' (Exit Code: %d)\n", timestamp.Format("Mon 15:04"), title, exitCode)
		}
	}

	if failedCommands == "" {
		failedCommands = "No failed commands recorded this week.\n"
	}

	// Extract daily stats from the last 7 days
	sevenDaysAgoTimeStr := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	statsRows, err := db.Query(`
		SELECT date, coding_minutes, learning_minutes, entertainment_minutes, focus_score, total_active_minutes
		FROM daily_stats
		WHERE date >= ?
		ORDER BY date ASC
	`, sevenDaysAgoTimeStr)
	
	if err != nil {
		fmt.Printf("❌ Failed to query daily stats: %v\n", err)
		return
	}
	defer statsRows.Close()

	dailyStats := ""
	for statsRows.Next() {
		var date string
		var cod, lrn, ent, act int
		var focus float64
		if err := statsRows.Scan(&date, &cod, &lrn, &ent, &focus, &act); err == nil {
			dailyStats += fmt.Sprintf("- [%s] Code: %dm, Learn: %dm, Distracted: %dm, Focus: %.1f%%, Active: %dm\n", date, cod, lrn, ent, focus, act)
		}
	}
	if dailyStats == "" {
		dailyStats = "No daily stats recorded this week.\n"
	}

	context := `You are AXIOM, a brutal DevSecOps and productivity mentor.
I am providing you with my productivity stats and failed terminal commands over the last 7 days.
Analyze my habits, point out where I'm wasting time, and identify any terminal command "Debugging Loops".
Generate a concise Markdown "Friction & Productivity Report" with harsh but actionable advice so I stop failing.`

	promptTrigger := fmt.Sprintf("Here are my failed terminal commands:\n%s\n\nHere are my daily stats:\n%s\nGive me the Friction & Productivity Report.", failedCommands, dailyStats)

	fmt.Println("🧠 Processing through Qwen3:4b (Thinking Mode)...")
	response, err := llm.AskAxiom(promptTrigger, context, true)
	if err != nil {
		fmt.Printf("❌ LLM Error: %v\n", err)
		return
	}

	fmt.Println("\n" + response)
}
