package filewatcher

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"axiom/pkg/event"
	"axiom/pkg/goals"

	"github.com/fsnotify/fsnotify"
)

type Config struct {
	Goals     *goals.Goals
	EventChan chan<- event.Event
}

// recentEvent represents an event in the debounce cache
type recentEvent struct {
	path      string
	timestamp time.Time
}

func Run(ctx context.Context, cfg Config) error {
	log.Println("📁 Starting FileWatcher sensor...")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// Debounce map to prevent IDE save storms
	var mu sync.Mutex
	debounceMap := make(map[string]time.Time)

	// Clean up debounce map periodically
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				now := time.Now()
				for k, v := range debounceMap {
					if now.Sub(v) > 10*time.Second {
						delete(debounceMap, k)
					}
				}
				mu.Unlock()
			}
		}
	}()

	// Add directories to watch
	if cfg.Goals != nil {
		for _, proj := range cfg.Goals.Projects {
			if proj.RepositoryPath != "" {
				// We need to recursively add directories
				err = filepath.Walk(proj.RepositoryPath, func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return nil // ignore errors like permissions
					}
					if info.IsDir() {
						// Skip common heavy directories
						name := info.Name()
						if name == ".git" {
							// Explicitly watch just the root of .git for HEAD/index changes, but skip walking subdirs
							if err := watcher.Add(path); err != nil {
								log.Printf("⚠️ Failed to watch .git directory %s: %v", path, err)
							}
							return filepath.SkipDir
						}
						if name == "node_modules" || name == "vendor" || name == "__pycache__" || name == ".idea" || name == ".vscode" {
							return filepath.SkipDir
						}
						err = watcher.Add(path)
						if err != nil {
							log.Printf("⚠️ Failed to watch directory %s: %v", path, err)
						}
					}
					return nil
				})
				if err != nil {
					log.Printf("⚠️ Error walking project %s for watcher: %v", proj.Name, err)
				} else {
					log.Printf("👀 Watching project directory: %s", proj.RepositoryPath)
				}
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 FileWatcher sensor stopping...")
			return nil

		case fwEvent, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// We are only interested in Write or Create events
			if !fwEvent.Has(fsnotify.Write) && !fwEvent.Has(fsnotify.Create) {
				continue
			}

			// Check if it's a directory (if created, we should add it to watcher)
			if fwEvent.Has(fsnotify.Create) {
				info, err := os.Stat(fwEvent.Name)
				if err == nil && info.IsDir() {
					watcher.Add(fwEvent.Name)
					continue
				}
			}

			// Debounce check
			mu.Lock()
			lastTime, exists := debounceMap[fwEvent.Name]
			now := time.Now()
			if exists && now.Sub(lastTime) < 2*time.Second {
				mu.Unlock()
				continue // Skip duplicate
			}
			debounceMap[fwEvent.Name] = now
			mu.Unlock()

			// Ignore temporary or lock files
			name := filepath.Base(fwEvent.Name)
			if strings.HasSuffix(name, "~") || strings.HasSuffix(name, ".swp") || strings.HasPrefix(name, ".goutputstream") || strings.HasSuffix(name, ".lock") {
				continue
			}

			// Cross-reference with projects
			project, isRelevant := "", 0
			repoPath := ""
			if cfg.Goals != nil {
				projName, matched := cfg.Goals.GetProjectForPath(fwEvent.Name)
				if matched {
					project = projName
					isRelevant = 1
					for _, p := range cfg.Goals.Projects {
						if p.Name == projName {
							repoPath = p.RepositoryPath
							break
						}
					}
				}
			}

			// Detect git commit or checkout (Zero-Load Git Monitor)
			if strings.Contains(fwEvent.Name, string(filepath.Separator)+".git"+string(filepath.Separator)) || strings.HasSuffix(fwEvent.Name, string(filepath.Separator)+".git") {
				if name == "index" || name == "HEAD" || name == "COMMIT_EDITMSG" {
					
					// Get basic git status quickly
					gitSummary := "Git operation"
					if repoPath != "" {
						cmd := exec.Command("git", "log", "-1", "--oneline")
						cmd.Dir = repoPath
						out, err := cmd.Output()
						if err == nil {
							gitSummary = strings.TrimSpace(string(out))
						}
					}

					e := event.Event{
						Type:         "git_activity",
						Source:       "gitmonitor",
						Time:         now,
						Title:        gitSummary,
						Category:     "coding",
						Project:      project,
						GoalRelevant: isRelevant,
						Metadata: map[string]string{
							"git_file": name,
							"repo": repoPath,
						},
					}

					select {
					case cfg.EventChan <- e:
					default:
					}
				}
				// Skip normal file tracking for .git files
				continue
			}

			// Emit event
			e := event.Event{
				Type:         "file_changed",
				Source:       "filewatcher",
				Time:         now,
				Title:        fwEvent.Name, // Using Title for file path for consistency
				Category:     "coding",     // Default for file changes in repos
				Project:      project,
				GoalRelevant: isRelevant,
				Metadata: map[string]string{
					"operation": fwEvent.Op.String(),
				},
			}

			select {
			case cfg.EventChan <- e:
			default:
				log.Println("⚠️ FileWatcher: Event pipeline is full, dropping event")
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("❌ FileWatcher error: %v", err)
		}
	}
}
