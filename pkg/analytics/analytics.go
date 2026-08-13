package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"axiom/pkg/goals"
	"axiom/pkg/llm"
)

// RollupWorker runs continuously in the background and aggregates raw events into daily stats
func RollupWorker(ctx context.Context, db *sql.DB, goalsConfig *goals.Goals) {
	log.Println("📈 Analytics Rollup worker started...")
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Run an initial rollup on startup
	if err := performRollup(db, goalsConfig); err != nil {
		log.Printf("⚠️ Initial analytics rollup error: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Stopping Analytics Rollup worker. Performing final flush...")
			if err := performRollup(db, goalsConfig); err != nil {
				log.Printf("❌ Final analytics rollup error: %v", err)
			}
			return
		case <-ticker.C:
			if err := performRollup(db, goalsConfig); err != nil {
				log.Printf("⚠️ Analytics rollup error: %v", err)
			} else {
				triggerActiveIntervention(db)
			}
		}
	}
}

func triggerActiveIntervention(db *sql.DB) {
	var currentFocus float64
	var nullMessage sql.NullString
	
	err := db.QueryRow("SELECT focus_score, axiom_summary FROM daily_stats WHERE date = date('now', 'localtime')").Scan(&currentFocus, &nullMessage)
	
	if err == nil && currentFocus > 0 && currentFocus < 95.0 && (!nullMessage.Valid || nullMessage.String == "" || nullMessage.String == "Awaiting data." || nullMessage.String == "Your focus is excellent right now! Keep it up.") {
		// Only roast once per failure cycle (if message hasn't been updated yet)
		log.Println("\n⚠️ [AXIOM LLM] 🔴 Focus is below 95%. Waking up Qwen3:4B to intervene...")

		// Fetch recent context for the LLM to make the roast hyper-specific
		var recentActivities string
		rows, qErr := db.Query(`
			SELECT app, title, duration_ms 
			FROM events 
			WHERE date(timestamp / 1000, 'unixepoch', 'localtime') = date('now', 'localtime')
			AND category = 'entertainment'
			AND duration_ms > 5000
			ORDER BY timestamp DESC
			LIMIT 3
		`)
		if qErr == nil {
			for rows.Next() {
				var app, title string
				var dur int
				rows.Scan(&app, &title, &dur)
				// Clean empty titles
				if title == "" {
					title = "Unknown Window"
				}
				recentActivities += fmt.Sprintf("- Spent %d seconds in %s: \"%s\"\n", dur/1000, app, title)
			}
			rows.Close()
		}
		
		go func(focus float64, activities string) {
			triggerCtx := fmt.Sprintf("User focus has dropped to %.1f%%. They are distracted.", focus)
			if activities != "" {
				triggerCtx += "\nRecent specific distractions to roast them about:\n" + activities
			}
			log.Printf("🤖 [AXIOM LLM] Thinking about: %s\n", triggerCtx)
			
			// Ask LLM (Mascot mode - fast)
			roast, llmErr := llm.AskAxiom("Focus is terrible right now.", triggerCtx, false)
			if llmErr != nil {
				log.Printf("❌ [AXIOM LLM] Failed to generate roast: %v\n", llmErr)
				return
			}
			
			log.Printf("🗣️ [AXIOM LLM] Final Roast: \"%s\"\n", roast)
			
			// Save the roast to DB so the mascot displays it
			_, dbErr := db.Exec("UPDATE daily_stats SET axiom_summary = ? WHERE date = date('now', 'localtime')", roast)
			if dbErr != nil {
				log.Printf("❌ [AXIOM DB] Failed to save roast: %v\n", dbErr)
			}
		}(currentFocus, recentActivities)
	}
}

// performRollup executes highly optimized SQLite UPSERTs to calculate daily rollups
func performRollup(db *sql.DB, goalsConfig *goals.Goals) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	defer tx.Rollback()

	// ════════════════════════════════════════════════════════
	// 1. CORE TIME TRACKING — Category durations + domain-specific breakdowns
	// ════════════════════════════════════════════════════════
	dailyDurationsQuery := `
		INSERT INTO daily_stats (
			date, coding_minutes, entertainment_minutes, music_minutes, learning_minutes, 
			communication_minutes, youtube_minutes, reddit_minutes, 
			github_minutes, stackoverflow_visits
		)
		SELECT 
			date(timestamp / 1000, 'unixepoch', 'localtime') as day,
			SUM(CASE WHEN category = 'coding' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN category = 'entertainment' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN category = 'music' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN category = 'learning' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN category = 'communication' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN domain LIKE '%youtube%' OR title LIKE '%YouTube%' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN domain LIKE '%reddit%' OR title LIKE '%Reddit%' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN domain LIKE '%github%' OR title LIKE '%GitHub%' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN (domain LIKE '%stackoverflow%' OR title LIKE '%Stack Overflow%') AND event_type = 'foreground_session' THEN 1 ELSE 0 END)
		FROM events
		WHERE event_type IN ('foreground_session', 'audio_session', 'background_media_session')
		AND date(timestamp / 1000, 'unixepoch', 'localtime') = date('now', 'localtime')
		GROUP BY day
		ON CONFLICT(date) DO UPDATE SET
			coding_minutes = excluded.coding_minutes,
			entertainment_minutes = excluded.entertainment_minutes,
			music_minutes = excluded.music_minutes,
			learning_minutes = excluded.learning_minutes,
			communication_minutes = excluded.communication_minutes,
			youtube_minutes = excluded.youtube_minutes,
			reddit_minutes = excluded.reddit_minutes,
			github_minutes = excluded.github_minutes,
			stackoverflow_visits = excluded.stackoverflow_visits;
	`
	if _, err := tx.Exec(dailyDurationsQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 2. FOCUS SCORE — Ratio of productive time to total active time
	// ════════════════════════════════════════════════════════
	focusScoreQuery := `
		UPDATE daily_stats 
		SET 
			total_active_minutes = coding_minutes + entertainment_minutes + learning_minutes + communication_minutes,
			focus_score = CASE 
				WHEN (coding_minutes + entertainment_minutes + learning_minutes + communication_minutes) > 0 
				THEN ROUND(
					(CAST(coding_minutes + learning_minutes AS REAL) / 
					 CAST(coding_minutes + entertainment_minutes + learning_minutes + communication_minutes AS REAL)) * 100.0, 1
				)
				ELSE 0 
			END,
			target_met = CASE 
				WHEN coding_minutes >= (coding_hours_target * 60) THEN 1 
				ELSE 0 
			END
		WHERE date = date('now', 'localtime');
	`
	if _, err := tx.Exec(focusScoreQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 2b. IDLE MINUTES — Time spent in idle_open state
	// ════════════════════════════════════════════════════════
	idleQuery := `
		UPDATE daily_stats SET idle_minutes = COALESCE((
			SELECT SUM(duration_ms) / 60000 FROM events
			WHERE activity = 'idle_open'
			AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
		), 0)
		WHERE date = date('now', 'localtime');
	`
	if _, err := tx.Exec(idleQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 3. TERMINAL METRICS — Commands run, failed, and commits detected
	// ════════════════════════════════════════════════════════
	dailyCommandsQuery := `
		INSERT INTO daily_stats (date, commands_run, commands_failed, commits)
		SELECT 
			date(timestamp / 1000, 'unixepoch', 'localtime') as day,
			COUNT(*),
			SUM(CASE WHEN exit_code > 0 THEN 1 ELSE 0 END),
			SUM(CASE WHEN title LIKE '%git commit%' OR title LIKE '%git push%' THEN 1 ELSE 0 END)
		FROM events
		WHERE event_type = 'terminal_command'
		AND date(timestamp / 1000, 'unixepoch', 'localtime') = date('now', 'localtime')
		GROUP BY day
		ON CONFLICT(date) DO UPDATE SET
			commands_run = excluded.commands_run,
			commands_failed = excluded.commands_failed,
			commits = excluded.commits;
	`
	if _, err := tx.Exec(dailyCommandsQuery); err != nil {
		return err
	}

	// Add git_activity commits to commit count
	gitCommitsQuery := `
		UPDATE daily_stats SET commits = commits + COALESCE((
			SELECT COUNT(*) FROM events 
			WHERE event_type = 'git_activity' 
			AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
		), 0)
		WHERE date = date('now', 'localtime');
	`
	if _, err := tx.Exec(gitCommitsQuery); err != nil {
		// Non-fatal; continue
		log.Printf("⚠️ git commits rollup warning: %v", err)
	}

	// ════════════════════════════════════════════════════════
	// 4. SESSION ANALYTICS — Longest coding session, avg session, context switches
	// ════════════════════════════════════════════════════════
	sessionQuery := `
		UPDATE daily_stats SET
			longest_coding_session_min = COALESCE((
				SELECT MAX(duration_ms) / 60000 FROM events
				WHERE category = 'coding' AND event_type = 'foreground_session'
				AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
			), 0),
			avg_session_length_min = COALESCE((
				SELECT ROUND(AVG(duration_ms) / 60000.0, 1) FROM events
				WHERE event_type = 'foreground_session' AND duration_ms > 5000
				AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
			), 0),
			context_switches = COALESCE((
				SELECT COUNT(*) FROM events
				WHERE event_type = 'foreground_session'
				AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
			), 0),
			unique_apps_used = COALESCE((
				SELECT COUNT(DISTINCT app) FROM events
				WHERE event_type = 'foreground_session' AND app != ''
				AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
			), 0)
		WHERE date = date('now', 'localtime');
	`
	if _, err := tx.Exec(sessionQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 5. TIME BOUNDARIES — First/last activity + late night coding detection
	// ════════════════════════════════════════════════════════
	timeBoundaryQuery := `
		UPDATE daily_stats SET
			first_activity_time = COALESCE((
				SELECT time(MIN(timestamp) / 1000, 'unixepoch', 'localtime') FROM events
				WHERE event_type = 'foreground_session'
				AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
			), first_activity_time),
			last_activity_time = COALESCE((
				SELECT time(MAX(timestamp) / 1000, 'unixepoch', 'localtime') FROM events
				WHERE event_type = 'foreground_session'
				AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
			), last_activity_time),
			late_night_minutes = COALESCE((
				SELECT SUM(duration_ms) / 60000 FROM events
				WHERE category = 'coding' AND event_type = 'foreground_session'
				AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
				AND CAST(strftime('%H', timestamp / 1000, 'unixepoch', 'localtime') AS INTEGER) < 5
			), 0)
		WHERE date = date('now', 'localtime');
	`
	if _, err := tx.Exec(timeBoundaryQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 6. PEAK CODING HOUR — Which hour had the most coding activity
	// ════════════════════════════════════════════════════════
	peakHourQuery := `
		UPDATE daily_stats SET peak_coding_hour = COALESCE((
			SELECT CAST(strftime('%H', timestamp / 1000, 'unixepoch', 'localtime') AS INTEGER) as hr
			FROM events
			WHERE category = 'coding' AND event_type = 'foreground_session'
			AND date(timestamp / 1000, 'unixepoch', 'localtime') = daily_stats.date
			GROUP BY hr
			ORDER BY SUM(duration_ms) DESC
			LIMIT 1
		), -1);
	`
	if _, err := tx.Exec(peakHourQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 7. STREAKS — Consecutive days of coding/commits (calculated per-date)
	// ════════════════════════════════════════════════════════
	codingStreakQuery := `
		UPDATE daily_stats SET coding_streak_days = (
			WITH RECURSIVE streak(d, cnt) AS (
				SELECT daily_stats.date, 1
				UNION ALL
				SELECT date(d, '-1 day'), cnt + 1
				FROM streak
				WHERE EXISTS (
					SELECT 1 FROM daily_stats ds 
					WHERE ds.date = date(d, '-1 day') AND ds.coding_minutes > 0
				)
			)
			SELECT MAX(cnt) FROM streak
		) WHERE coding_minutes > 0;
	`
	if _, err := tx.Exec(codingStreakQuery); err != nil {
		// Non-fatal — recursive CTE may be complex on older SQLite
		log.Printf("⚠️ Coding streak calculation warning: %v", err)
	}

	commitStreakQuery := `
		UPDATE daily_stats SET commit_streak_days = (
			WITH RECURSIVE streak(d, cnt) AS (
				SELECT daily_stats.date, 1
				UNION ALL
				SELECT date(d, '-1 day'), cnt + 1
				FROM streak
				WHERE EXISTS (
					SELECT 1 FROM daily_stats ds 
					WHERE ds.date = date(d, '-1 day') AND ds.commits > 0
				)
			)
			SELECT MAX(cnt) FROM streak
		) WHERE commits > 0;
	`
	if _, err := tx.Exec(commitStreakQuery); err != nil {
		log.Printf("⚠️ Commit streak calculation warning: %v", err)
	}

	// ════════════════════════════════════════════════════════
	// 8. HOURLY HEATMAP — Per-hour breakdown for productivity heatmap
	// ════════════════════════════════════════════════════════
	hourlyHeatmapQuery := `
		INSERT INTO hourly_heatmap (date, hour, coding_minutes, entertainment_minutes, learning_minutes, total_minutes, context_switches, events_count)
		SELECT
			date(timestamp / 1000, 'unixepoch', 'localtime') as day,
			CAST(strftime('%H', timestamp / 1000, 'unixepoch', 'localtime') AS INTEGER) as hr,
			SUM(CASE WHEN category = 'coding' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN category = 'entertainment' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN category = 'learning' THEN duration_ms ELSE 0 END) / 60000,
			SUM(duration_ms) / 60000,
			COUNT(DISTINCT app),
			COUNT(*)
		FROM events
		WHERE event_type IN ('foreground_session', 'audio_session')
		GROUP BY day, hr
		ON CONFLICT(date, hour) DO UPDATE SET
			coding_minutes = excluded.coding_minutes,
			entertainment_minutes = excluded.entertainment_minutes,
			learning_minutes = excluded.learning_minutes,
			total_minutes = excluded.total_minutes,
			context_switches = excluded.context_switches,
			events_count = excluded.events_count;
	`
	if _, err := tx.Exec(hourlyHeatmapQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 9. APP USAGE — Per-app daily breakdown for "Top Apps" dashboard
	// ════════════════════════════════════════════════════════
	appUsageQuery := `
		INSERT INTO app_usage (date, app_name, category, minutes, session_count)
		SELECT
			date(timestamp / 1000, 'unixepoch', 'localtime') as day,
			COALESCE(NULLIF(app, ''), 
				json_extract(metadata, '$.process_name'), 
				'unknown'
			) as app_name,
			category,
			SUM(duration_ms) / 60000,
			COUNT(*)
		FROM events
		WHERE event_type = 'foreground_session'
		GROUP BY day, app_name, category
		ON CONFLICT(date, app_name) DO UPDATE SET
			category = excluded.category,
			minutes = excluded.minutes,
			session_count = excluded.session_count;
	`
	if _, err := tx.Exec(appUsageQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 10. PROJECT ACTIVITY — Per-project daily breakdown
	// ════════════════════════════════════════════════════════
	projectActivityQuery := `
		INSERT INTO project_activity (date, project, minutes, files_changed, commits)
		SELECT 
			date(timestamp / 1000, 'unixepoch', 'localtime') as day,
			project,
			SUM(CASE WHEN event_type = 'foreground_session' THEN duration_ms ELSE 0 END) / 60000,
			SUM(CASE WHEN event_type = 'file_changed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN event_type = 'git_activity' THEN 1 
				 WHEN event_type = 'terminal_command' AND title LIKE '%git commit%' THEN 1 
				 ELSE 0 END)
		FROM events
		WHERE project != ''
		GROUP BY day, project
		ON CONFLICT(date, project) DO UPDATE SET
			minutes = excluded.minutes,
			files_changed = excluded.files_changed,
			commits = excluded.commits;
	`
	if _, err := tx.Exec(projectActivityQuery); err != nil {
		return err
	}

	// ════════════════════════════════════════════════════════
	// 11. GOAL PROGRESS — Track each project/cert against its deadline
	// ════════════════════════════════════════════════════════
	if goalsConfig != nil {
		today := time.Now().Format("2006-01-02")

		// Track projects
		for _, proj := range goalsConfig.Projects {
			if proj.Name == "" {
				continue
			}
			daysRemaining := 0
			if proj.TargetCompletion != "" {
				if target, err := time.Parse("2006-01-02", proj.TargetCompletion); err == nil {
					daysRemaining = int(time.Until(target).Hours() / 24)
					if daysRemaining < 0 {
						daysRemaining = 0
					}
				}
			}
			goalQuery := `
				INSERT INTO goal_progress (date, goal_name, goal_type, total_minutes, target_date, days_remaining, on_track)
				VALUES (?, ?, 'project', 
					COALESCE((SELECT SUM(minutes) FROM project_activity WHERE project = ?), 0),
					?, ?, 0
				)
				ON CONFLICT(date, goal_name) DO UPDATE SET
					total_minutes = excluded.total_minutes,
					target_date = excluded.target_date,
					days_remaining = excluded.days_remaining;
			`
			tx.Exec(goalQuery, today, proj.Name, proj.Name, proj.TargetCompletion, daysRemaining)
		}

		// Track certifications
		for _, cert := range goalsConfig.Certifications {
			if cert.Name == "" {
				continue
			}
			daysRemaining := 0
			examDate := cert.ExamDate
			if examDate != "" {
				if target, err := time.Parse("2006-01-02", examDate); err == nil {
					daysRemaining = int(time.Until(target).Hours() / 24)
					if daysRemaining < 0 {
						daysRemaining = 0
					}
				}
			}
			prepMinutesDone := int(cert.PrepHoursDone * 60)
			onTrack := 0
			if cert.PrepHoursNeeded > 0 && daysRemaining > 0 {
				// On track if current pace can finish remaining hours in remaining days
				remainingHours := cert.PrepHoursNeeded - cert.PrepHoursDone
				hoursPerDay := remainingHours / float64(daysRemaining)
				if hoursPerDay <= 2.0 { // reasonable daily study load
					onTrack = 1
				}
			}
			certQuery := `
				INSERT INTO goal_progress (date, goal_name, goal_type, total_minutes, target_date, days_remaining, on_track)
				VALUES (?, ?, 'certification', ?, ?, ?, ?)
				ON CONFLICT(date, goal_name) DO UPDATE SET
					total_minutes = excluded.total_minutes,
					target_date = excluded.target_date,
					days_remaining = excluded.days_remaining,
					on_track = excluded.on_track;
			`
			tx.Exec(certQuery, today, cert.Name, prepMinutesDone, examDate, daysRemaining, onTrack)
		}
	}

	return tx.Commit()
}
