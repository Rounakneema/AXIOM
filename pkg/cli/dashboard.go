package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// PrintTodayDashboard renders the entertaining and highly analytical CLI dashboard
func PrintTodayDashboard() {
	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".axiom", "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("❌ Failed to open AXIOM memory core: %v\n", err)
		return
	}
	defer db.Close()

	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║               AXIOM OS — Life Analytics Dashboard            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	dateStr := time.Now().Format("2006-01-02")
	fmt.Printf("\n📅 DATE: %s\n", dateStr)
	fmt.Println(strings.Repeat("─", 60))

	// ── 1. Daily Stats ──
	var codingMin, entMin, learnMin, commMin, ytMin, redditMin, ghMin int
	var soVisits, cmdsRun, cmdsFailed, commits int
	var focusScore, avgSession float64
	var totalActive, longestSession, ctxSwitches, uniqueApps int
	var lateNight, codingStreak, commitStreak, targetMet, peakHour int
	var firstAct, lastAct sql.NullString

	err = db.QueryRow(`
		SELECT coding_minutes, entertainment_minutes, learning_minutes, 
			   communication_minutes, youtube_minutes, reddit_minutes, github_minutes,
			   stackoverflow_visits, commands_run, commands_failed, commits,
			   focus_score, total_active_minutes, longest_coding_session_min,
			   avg_session_length_min, context_switches, unique_apps_used,
			   first_activity_time, last_activity_time,
			   late_night_minutes, coding_streak_days, commit_streak_days,
			   target_met, COALESCE(peak_coding_hour, -1)
		FROM daily_stats WHERE date = ?
	`, dateStr).Scan(
		&codingMin, &entMin, &learnMin, &commMin,
		&ytMin, &redditMin, &ghMin, &soVisits,
		&cmdsRun, &cmdsFailed, &commits,
		&focusScore, &totalActive, &longestSession,
		&avgSession, &ctxSwitches, &uniqueApps,
		&firstAct, &lastAct,
		&lateNight, &codingStreak, &commitStreak,
		&targetMet, &peakHour,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			fmt.Println("No analytics data recorded for today yet. Get to work!")
			return
		}
		fmt.Printf("Error retrieving stats: %v\n", err)
		return
	}

	targetEmoji := "❌"
	if targetMet == 1 {
		targetEmoji = "✅"
	}
	mood := "neutral"
	if focusScore >= 80 {
		mood = "god_mode"
	} else if focusScore < 40 && entMin > 60 {
		mood = "distracted"
	} else if lateNight > 60 {
		mood = "exhausted"
	}

	fmt.Printf("🧠 AXIOM Mood:     [%s]\n", strings.ToUpper(mood))
	fmt.Printf("🎯 Target Met:     %s\n", targetEmoji)
	fmt.Printf("🔥 Focus Score:    %.1f%%\n", focusScore)
	fmt.Printf("⏱️  Total Active:   %dm\n", totalActive)
	fmt.Println()

	fmt.Println("📊 CATEGORY BREAKDOWN")
	fmt.Printf("  ├── 💻 Coding:         %dm\n", codingMin)
	fmt.Printf("  ├── 📚 Learning:       %dm\n", learnMin)
	fmt.Printf("  ├── 🎮 Entertainment:  %dm\n", entMin)
	fmt.Printf("  └── 💬 Comms:          %dm\n", commMin)
	fmt.Println()

	fmt.Println("🌐 DOMAIN & TERMINAL METRICS")
	fmt.Printf("  ├── 📺 YouTube:        %dm\n", ytMin)
	fmt.Printf("  ├── 🛑 Reddit:         %dm\n", redditMin)
	fmt.Printf("  ├── 🐙 GitHub:         %dm\n", ghMin)
	fmt.Printf("  ├── 🆘 StackOverflow:  %d visits\n", soVisits)
	fmt.Printf("  ├── ⌨️  Terminal:       %d commands (%d failed)\n", cmdsRun, cmdsFailed)
	fmt.Printf("  └── 📦 Git Commits:    %d\n", commits)
	fmt.Println()

	fmt.Println("🔬 SESSION ANALYTICS")
	fmt.Printf("  ├── 📈 Longest Coding Session: %dm\n", longestSession)
	fmt.Printf("  ├── ⏱️  Avg Session Length:     %.1fm\n", avgSession)
	fmt.Printf("  ├── 🔀 Context Switches:       %d\n", ctxSwitches)
	fmt.Printf("  ├── 📱 Unique Apps Used:       %d\n", uniqueApps)
	fmt.Printf("  ├── 🌙 Late Night Coding:      %dm\n", lateNight)
	if peakHour != -1 {
		fmt.Printf("  └── ⭐ Peak Productivity Hour: %02d:00\n", peakHour)
	}
	fmt.Println()

	fmt.Println("🔥 STREAKS")
	fmt.Printf("  ├── 💻 Coding Streak: %d days\n", codingStreak)
	fmt.Printf("  └── 📦 Commit Streak: %d days\n", commitStreak)

	// ── 2. Hourly Heatmap ──
	fmt.Println("\n🕐 HOURLY HEATMAP (Today)")
	fmt.Println(strings.Repeat("─", 60))
	heatRows, err := db.Query(`
		SELECT hour, coding_minutes, entertainment_minutes, learning_minutes, context_switches
		FROM hourly_heatmap WHERE date = ? ORDER BY hour
	`, dateStr)
	if err == nil {
		defer heatRows.Close()
		for heatRows.Next() {
			var hour, cMin, eMin, lMin, sw int
			heatRows.Scan(&hour, &cMin, &eMin, &lMin, &sw)
			
			bar := ""
			for i := 0; i < cMin; i += 2 { bar += "█" }  // Coding
			for i := 0; i < lMin; i += 2 { bar += "▓" }  // Learning
			for i := 0; i < eMin; i += 2 { bar += "░" }  // Entertainment
			
			fmt.Printf("  %02d:00 │ %-30s │ code:%dm ent:%dm learn:%dm sw:%d\n", hour, bar, cMin, eMin, lMin, sw)
		}
	}

	// ── 3. App Usage ──
	fmt.Println("\n📱 TOP APPS (Today)")
	fmt.Println(strings.Repeat("─", 60))
	appRows, err := db.Query(`
		SELECT app_name, category, minutes, session_count 
		FROM app_usage WHERE date = ? ORDER BY minutes DESC LIMIT 5
	`, dateStr)
	if err == nil {
		defer appRows.Close()
		for appRows.Next() {
			var appName, cat string
			var mins, count int
			appRows.Scan(&appName, &cat, &mins, &count)
			fmt.Printf("  %-25s [%-13s] %3dm (%3d sessions)\n", appName, cat, mins, count)
		}
	}

	// ── 4. Goal Progress ──
	fmt.Println("\n🎯 GOAL PROGRESS (Deadlines)")
	fmt.Println(strings.Repeat("─", 60))
	goalRows, err := db.Query(`
		SELECT goal_name, goal_type, total_minutes, COALESCE(target_date, 'N/A'), days_remaining, on_track 
		FROM goal_progress WHERE date = ? ORDER BY days_remaining ASC
	`, dateStr)
	if err == nil {
		defer goalRows.Close()
		for goalRows.Next() {
			var name, typ, target string
			var mins, days, track int
			goalRows.Scan(&name, &typ, &mins, &target, &days, &track)
			trackEmoji := "⚠️"
			if track == 1 { trackEmoji = "✅" }
			fmt.Printf("  %s %-20s [%-13s] %3dh%02dm | Due: %s (%3d days)\n", trackEmoji, name, typ, mins/60, mins%60, target, days)
		}
	}

	fmt.Println("\n" + strings.Repeat("═", 60))
}
