package data

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"axiom/pkg/event"
	"axiom/pkg/goals"
	"strings"

	_ "modernc.org/sqlite"
)

// DBChan is the thread-safe global event funnel
var DBChan = make(chan event.Event, 1000)

// InitDB initializes the SQLite database, runs schemas migrations and configures PRAGMAs
func InitDB(dbPath string) (*sql.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Optimize SQLite settings for local performance and simultaneous reads
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA busy_timeout = 5000;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to apply pragma %q: %w", pragma, err)
		}
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration failure: %w", err)
	}

	return db, nil
}

// StartDBWorker runs the single-threaded write loop that processes the DBChan
func StartDBWorker(db *sql.DB, goalsConfig *goals.Goals) {
	log.Println("🚀 SQLite channel database worker started...")

	stmt, err := db.Prepare(`
		INSERT INTO events (
			timestamp, event_type, source, app, activity, confidence, 
			duration_ms, hwnd, pid, title, url, category, project, 
			goal_relevant, domain, exit_code, metadata
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	if err != nil {
		log.Fatalf("❌ Failed to prepare event insertion statement: %v", err)
	}
	defer stmt.Close()

	for ev := range DBChan {
		var metadataJSON string
		if ev.Metadata != nil {
			if b, err := json.Marshal(ev.Metadata); err == nil {
				metadataJSON = string(b)
			}
		}

		// Global categorization for all events
		if goalsConfig != nil && (ev.Category == "" || ev.Category == "unknown") {
			lowerApp := strings.ToLower(ev.App)
			isBrowser := strings.Contains(lowerApp, "chrome") || 
				strings.Contains(lowerApp, "firefox") || 
				strings.Contains(lowerApp, "msedge") || 
				strings.Contains(lowerApp, "brave") ||
				strings.Contains(lowerApp, "opera")

			var cat string
			if isBrowser {
				// For browsers: ALWAYS classify by title/URL first (the app name is meaningless)
				cat = goalsConfig.ClassifyDomain(ev.Title)
				if cat == "unknown" && ev.URL != "" {
					cat = goalsConfig.ClassifyDomain(ev.URL)
				}
				if cat == "unknown" {
					cat = "entertainment" // Default: if it's a browser and we can't identify it, assume distraction
				}
				log.Printf("🏷️ [CLASSIFY] Browser=%s Title=\"%.50s\" → %s", ev.App, ev.Title, cat)
			} else {
				// For desktop apps: classify by app name, then fallback to title
				cat = goalsConfig.ClassifyApp(ev.App)
				if cat == "unknown" {
					cat = goalsConfig.ClassifyDomain(ev.Title)
				}
			}
			ev.Category = cat
			
			// Auto-tag projects based on window titles
			for _, p := range goalsConfig.Projects {
				if p.Name != "" && strings.Contains(strings.ToLower(ev.Title), strings.ToLower(p.Name)) {
					ev.Project = p.Name
					ev.GoalRelevant = 1
					break
				}
			}

			if ev.GoalRelevant == 0 && (ev.Category == "coding" || ev.Category == "learning") {
				ev.GoalRelevant = 1
			}
		}

		_, err := stmt.Exec(
			ev.Time.UnixMilli(),
			ev.Type,
			ev.Source,
			ev.App,
			ev.Activity,
			ev.Confidence,
			ev.Duration.Milliseconds(),
			int64(ev.HWND),
			ev.PID,
			ev.Title,
			ev.URL,
			ev.Category,
			ev.Project,
			ev.GoalRelevant,
			ev.Domain,
			ev.ExitCode,
			metadataJSON,
		)
		if err != nil {
			log.Printf("❌ SQLite insertion error: %v", err)
		}
	}
	log.Println("🔌 SQLite database worker shutdown completed.")
}

func runMigrations(db *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp     INTEGER NOT NULL,
			event_type    TEXT NOT NULL,
			source        TEXT NOT NULL,
			app           TEXT,
			activity      TEXT,
			confidence    TEXT,
			duration_ms   INTEGER,
			hwnd          INTEGER,
			pid           INTEGER,
			title         TEXT,
			url           TEXT,
			category      TEXT,
			project       TEXT,
			goal_relevant INTEGER DEFAULT 0,
			domain        TEXT,
			exit_code     INTEGER,
			metadata      TEXT
		);`,
		// ── daily_stats: The master daily summary ──
		`CREATE TABLE IF NOT EXISTS daily_stats (
			date                    TEXT PRIMARY KEY,
			-- Core time tracking
			coding_minutes          INTEGER DEFAULT 0,
			entertainment_minutes   INTEGER DEFAULT 0,
			learning_minutes        INTEGER DEFAULT 0,
			idle_minutes            INTEGER DEFAULT 0,
			communication_minutes   INTEGER DEFAULT 0,
			-- Specific domain tracking
			youtube_minutes         INTEGER DEFAULT 0,
			reddit_minutes          INTEGER DEFAULT 0,
			github_minutes          INTEGER DEFAULT 0,
			stackoverflow_visits    INTEGER DEFAULT 0,
			-- Terminal metrics
			commands_run            INTEGER DEFAULT 0,
			commands_failed         INTEGER DEFAULT 0,
			commits                 INTEGER DEFAULT 0,
			-- Session & focus metrics
			focus_score             REAL DEFAULT 0,
			total_active_minutes    INTEGER DEFAULT 0,
			longest_coding_session_min INTEGER DEFAULT 0,
			avg_session_length_min  REAL DEFAULT 0,
			context_switches        INTEGER DEFAULT 0,
			unique_apps_used        INTEGER DEFAULT 0,
			-- Time boundaries
			first_activity_time     TEXT,
			last_activity_time      TEXT,
			late_night_minutes      INTEGER DEFAULT 0,
			-- Streaks & targets
			coding_hours_target     REAL DEFAULT 4,
			target_met              INTEGER DEFAULT 0,
			coding_streak_days      INTEGER DEFAULT 0,
			commit_streak_days      INTEGER DEFAULT 0,
			-- Git metrics
			git_additions           INTEGER DEFAULT 0,
			git_deletions           INTEGER DEFAULT 0,
			-- AXIOM personality
			axiom_mood_eod          TEXT,
			axiom_summary           TEXT,
			-- Peak hour (0-23)
			peak_coding_hour        INTEGER DEFAULT -1
		);`,
		// ── project_activity: Per-project daily breakdown ──
		`CREATE TABLE IF NOT EXISTS project_activity (
			date          TEXT NOT NULL,
			project       TEXT NOT NULL,
			minutes       INTEGER DEFAULT 0,
			commits       INTEGER DEFAULT 0,
			files_changed INTEGER DEFAULT 0,
			PRIMARY KEY (date, project)
		);`,
		// ── hourly_heatmap: Per-hour productivity breakdown ──
		`CREATE TABLE IF NOT EXISTS hourly_heatmap (
			date          TEXT NOT NULL,
			hour          INTEGER NOT NULL,
			coding_minutes    INTEGER DEFAULT 0,
			entertainment_minutes INTEGER DEFAULT 0,
			learning_minutes  INTEGER DEFAULT 0,
			total_minutes     INTEGER DEFAULT 0,
			context_switches  INTEGER DEFAULT 0,
			events_count      INTEGER DEFAULT 0,
			PRIMARY KEY (date, hour)
		);`,
		// ── app_usage: Per-app daily breakdown (top apps dashboard) ──
		`CREATE TABLE IF NOT EXISTS app_usage (
			date          TEXT NOT NULL,
			app_name      TEXT NOT NULL,
			category      TEXT,
			minutes       INTEGER DEFAULT 0,
			session_count INTEGER DEFAULT 0,
			PRIMARY KEY (date, app_name)
		);`,
		// ── goal_progress: Daily snapshot of how each project/cert tracks against deadline ──
		`CREATE TABLE IF NOT EXISTS goal_progress (
			date              TEXT NOT NULL,
			goal_name         TEXT NOT NULL,
			goal_type         TEXT NOT NULL,
			total_minutes     INTEGER DEFAULT 0,
			target_date       TEXT,
			days_remaining    INTEGER DEFAULT 0,
			on_track          INTEGER DEFAULT 0,
			PRIMARY KEY (date, goal_name)
		);`,
		// ── patterns, conversations, axiom_state (unchanged) ──
		`CREATE TABLE IF NOT EXISTS patterns (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			name           TEXT NOT NULL,
			first_detected TEXT NOT NULL,
			last_detected  TEXT NOT NULL,
			occurrences    INTEGER DEFAULT 1,
			description    TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp   INTEGER NOT NULL,
			trigger     TEXT,
			context     TEXT,
			response    TEXT,
			mood        TEXT,
			helpful     INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS axiom_state (
			key   TEXT PRIMARY KEY,
			value TEXT
		);`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			return err
		}
	}

	// ── ALTER TABLE migrations for existing databases ──
	// These silently fail if columns already exist, which is safe.
	alterMigrations := []string{
		"ALTER TABLE daily_stats ADD COLUMN total_active_minutes INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN longest_coding_session_min INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN avg_session_length_min REAL DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN context_switches INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN unique_apps_used INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN first_activity_time TEXT;",
		"ALTER TABLE daily_stats ADD COLUMN last_activity_time TEXT;",
		"ALTER TABLE daily_stats ADD COLUMN late_night_minutes INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN coding_streak_days INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN commit_streak_days INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN git_additions INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN git_deletions INTEGER DEFAULT 0;",
		"ALTER TABLE daily_stats ADD COLUMN peak_coding_hour INTEGER DEFAULT -1;",
		"ALTER TABLE daily_stats ADD COLUMN github_minutes INTEGER DEFAULT 0;",
	}
	for _, a := range alterMigrations {
		db.Exec(a) // intentionally ignore errors — column may already exist
	}

	// Create indices for faster analytics queries
	indices := []string{
		"CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);",
		"CREATE INDEX IF NOT EXISTS idx_events_category ON events(category);",
		"CREATE INDEX IF NOT EXISTS idx_events_project ON events(project);",
		"CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);",
		"CREATE INDEX IF NOT EXISTS idx_events_app ON events(app);",
		"CREATE INDEX IF NOT EXISTS idx_events_domain ON events(domain);",
		"CREATE INDEX IF NOT EXISTS idx_events_type_ts ON events(event_type, timestamp);",
	}
	for _, idx := range indices {
		if _, err := db.Exec(idx); err != nil {
			return err
		}
	}

	return nil
}
