package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"axiom/pkg/event"
	"axiom/pkg/goals"
	_ "modernc.org/sqlite"
)

type BrowserPayload struct {
	URL      string `json:"url"`
	Title    string `json:"title"`
	Domain   string `json:"domain"`
	App      string `json:"app"`
	Activity string `json:"activity"`
	Duration int    `json:"duration"`
}

// StartServer launches the local HTTP API to receive events from Chrome/Edge and serve state to the Mascot
func StartServer(port int, goalsConfig *goals.Goals, dbChan chan<- event.Event) error {
	mux := http.NewServeMux()

	home, _ := os.UserHomeDir()
	dbPath := filepath.Join(home, ".axiom", "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("⚠️ API: Could not open memory.db for Mascot polling")
	} else {
		// Enable WAL and busy_timeout to prevent locking conflicts with the main RollupWorker
		db.Exec("PRAGMA journal_mode=WAL;")
		db.Exec("PRAGMA busy_timeout=5000;")
	}

	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if db == nil {
			http.Error(w, `{"error": "db not connected"}`, http.StatusInternalServerError)
			return
		}

		dateStr := time.Now().Format("2006-01-02")
		var focusScore float64
		var codingMin int
		var entMin int
		var musicMin int
		var nullMood sql.NullString
		var nullMessage sql.NullString

		// Get today's stats
		err := db.QueryRow(`
			SELECT focus_score, coding_minutes, entertainment_minutes, music_minutes, axiom_mood_eod, axiom_summary
			FROM daily_stats WHERE date = ?
		`, dateStr).Scan(&focusScore, &codingMin, &entMin, &musicMin, &nullMood, &nullMessage)

		mood := "Neutral"
		if nullMood.Valid && nullMood.String != "" {
			mood = nullMood.String
		}
		
		message := "Your focus is excellent right now! Keep it up."
		if nullMessage.Valid && nullMessage.String != "" {
			message = nullMessage.String
		}

		if err != nil {
			// Fallback: Compute real-time from today's raw events
			var totalProductive float64
			var totalDistracted float64
			
			db.QueryRow(`
				SELECT 
					COALESCE(SUM(CASE WHEN category IN ('coding', 'learning') THEN duration_ms ELSE 0 END), 0),
					COALESCE(SUM(CASE WHEN category IN ('entertainment', 'communication') THEN duration_ms ELSE 0 END), 0)
				FROM events 
				WHERE date(timestamp / 1000, 'unixepoch', 'localtime') = ?
			`, dateStr).Scan(&totalProductive, &totalDistracted)

			total := totalProductive + totalDistracted
			if total > 0 {
				focusScore = (totalProductive / total) * 100.0
			} else {
				focusScore = 100.0 // True default if literally 0 events logged yet
			}

			mood = "Neutral"
			message = "Awaiting data."
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"focus_score": focusScore,
			"coding_minutes": codingMin,
			"entertainment_minutes": entMin,
			"music_minutes": musicMin,
			"mood": mood,
			"message": message,
		})
	})

	mux.HandleFunc("/api/browser-event", func(w http.ResponseWriter, r *http.Request) {
		// Handle CORS for browser extensions
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload BrowserPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Extract domain properly if not provided
		domain := payload.Domain
		if domain == "" {
			if parsed, err := url.Parse(payload.URL); err == nil {
				domain = parsed.Hostname()
			}
			if domain == "" {
				domain = payload.URL
			}
		}

		// Create the event
		evt := event.Event{
			Type:       "foreground_session", // We treat this as a definitive foreground session
			Source:     "browser_extension",
			App:        payload.App,
			Activity:   payload.Activity,
			Confidence: "high", // 100% accurate because it's from the extension API
			Time:       time.Now(),
			Title:      payload.Title,
			URL:        payload.URL,
			Domain:     domain,
		}

		if payload.App == "" {
			evt.App = "chrome.exe"
		}

		// Real-time tagging engine for the URL context
		if goalsConfig != nil {
			cat := goalsConfig.ClassifyDomain(evt.URL)
			if cat == "unknown" && evt.Title != "" {
				cat = goalsConfig.ClassifyDomain(evt.Title)
			}
			evt.Category = cat

			// Match project based on URL or title
			if proj, ok := goalsConfig.GetProjectForPath(evt.URL); ok {
				evt.Project = proj
				evt.GoalRelevant = 1
			} else {
				for _, p := range goalsConfig.Projects {
					if strings.Contains(strings.ToLower(evt.Title), strings.ToLower(p.Name)) {
						evt.Project = p.Name
						evt.GoalRelevant = 1
						break
					}
				}
			}

			// Learning/Coding override
			if evt.GoalRelevant == 0 && (evt.Category == "coding" || evt.Category == "learning") {
				evt.GoalRelevant = 1
			}
		} else {
			evt.Category = "unknown"
		}

		// If duration is passed, it means the session just closed, so we set EndTime
		if payload.Duration > 0 {
			evt.Duration = time.Duration(payload.Duration) * time.Millisecond
			evt.EndTime = evt.Time // EndTime is now, StartTime is implicitly EndTime - Duration for SQLite
			evt.Time = evt.EndTime.Add(-evt.Duration)
		} else {
			// Instantaneous event tracking
			evt.Duration = 0
		}

		// Send to the DB writer queue
		select {
		case dbChan <- evt:
			// Success
		default:
			log.Println("⚠️ API: DB Queue channel full, dropped browser event")
		}

		// Optionally return JSON with intent-checker status
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"status":   "recorded",
			"category": evt.Category,
		}

		// If it's entertainment, trigger the Intent Checker in the extension
		if evt.Category == "entertainment" {
			response["trigger_intent_check"] = true
		} else {
			response["trigger_intent_check"] = false
		}

		json.NewEncoder(w).Encode(response)
	})

	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)
	log.Printf("🌐 Starting AXIOM Local API Server on %s", serverAddr)
	
	// Start server (blocking)
	return http.ListenAndServe(serverAddr, mux)
}
