package goals

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type WorkingHours struct {
	StartTime        string   `yaml:"startTime"`
	EndTime          string   `yaml:"endTime"`
	Timezone         string   `yaml:"timezone"`
	PeakFocusWindows []Window `yaml:"peak_focus_windows"`
}

type Window struct {
	Start  string  `yaml:"start"`
	End    string  `yaml:"end"`
	Weight float64 `yaml:"weight"`
}

type Identity struct {
	Name             string       `yaml:"name"`
	CurrentRole      string       `yaml:"current_role"`
	PrimaryDomain    string       `yaml:"primary_domain"`
	SubDomain        string       `yaml:"sub_domain"`
	TargetRole       string       `yaml:"target_role"`
	Graduation       string       `yaml:"graduation"`
	Location         string       `yaml:"location"`
	PortfolioURL     string       `yaml:"portfolio_url"`
	GithubUsername   string       `yaml:"github_username"`
	LinkedinUsername string       `yaml:"linkedin_username"`
	WorkingHours     WorkingHours `yaml:"working_hours"`
}

type CareerTarget struct {
	PrimaryGoal     string   `yaml:"primary_goals"`
	SalaryTarget    string   `yaml:"salary_targets"`
	Timeline        string   `yaml:"timeline"`
	TargetCompanies []string `yaml:"target_companies"`
	CoreSkills      []string `yaml:"core_skills"`
	AdvancedSkills  []string `yaml:"advanced_skills"`
}

type Project struct {
	Name             string           `yaml:"name"`
	Status           string           `yaml:"status"`
	Priority         int              `yaml:"priority"`
	Description      string           `yaml:"description"`
	TargetCompletion string           `yaml:"target_completion"`
	RepositoryPath   string           `yaml:"repository_path"`
	DirectoryAliases []string         `yaml:"directory_aliases"`
	MustHaveFeatures []string         `yaml:"must_have_features"`
	FileWatcherRules FileWatcherRules `yaml:"file_watcher_rules"`
}

type FileWatcherRules struct {
	IncludedExtensions []string `yaml:"included_extensions"`
	ExcludedPaths      []string `yaml:"excluded_paths"`
}

type Certification struct {
	Name              string   `yaml:"name"`
	Status            string   `yaml:"status"`
	ExamDate          string   `yaml:"exam_date"`
	PrepHoursNeeded   float64  `yaml:"prep_hours_needed"`
	PrepHoursDone     float64  `yaml:"prep_hours_done"`
	SyllabusFocus     []string `yaml:"syllabus_focus"`
	LearningResources []string `yaml:"learning_resources"`
}

type DistractionRules struct {
	BlacklistedProcesses []string `yaml:"blacklisted_processes"`
	BlacklistedDomains   []string `yaml:"blacklisted_domains"`
	WhitelistedDomains   []string `yaml:"whitelisted_domains"`
}

type CategorizationRules struct {
	Applications    map[string][]string `yaml:"applications"`
	Domains         map[string][]string `yaml:"domains"`
	YouTubeKeywords []string            `yaml:"youtube_learning_keywords"`
}

type LimitsAndTargets struct {
	MinCodingHours          float64             `yaml:"min_coding_hours"`
	MinProjectCommits       int                 `yaml:"min_project_commits"`
	MaxEntertainmentPercent float64             `yaml:"max_entertainment_percent"`
	StudySessionsPerWeek    int                 `yaml:"study_sessions_per_week"`
	CategorizationRules     CategorizationRules `yaml:"categorization_rules"`
}

type Goals struct {
	Identity         Identity         `yaml:"identity"`
	CareerTarget     CareerTarget     `yaml:"career_targets"`
	Projects         []Project        `yaml:"projects"`
	Certifications   []Certification  `yaml:"certifications"`
	LimitsAndTargets LimitsAndTargets `yaml:"limits_and_targets"`
	DistractionRules DistractionRules `yaml:"distraction_rules"`
}

// LoadGoals reads and parses the goals yaml file
func LoadGoals(path string) (*Goals, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var g Goals
	if err := yaml.Unmarshal(data, &g); err != nil {
		return nil, err
	}

	return &g, nil
}

// GetSystemPrompt generates an intelligent context prompt based on the user's configured goals
func (g *Goals) GetSystemPrompt() string {
	var sb strings.Builder
	
	name := g.Identity.Name
	if name == "" {
		name = "the user"
	}
	
	sb.WriteString(fmt.Sprintf("You are AXIOM, a brutal and strict local AI assistant for %s.\n", name))
	
	if g.Identity.CurrentRole != "" {
		sb.WriteString(fmt.Sprintf("The user is currently a %s, ", g.Identity.CurrentRole))
	}
	if g.Identity.TargetRole != "" {
		sb.WriteString(fmt.Sprintf("trying to become a %s.\n", g.Identity.TargetRole))
	}
	
	sb.WriteString("Your job is to hold them accountable to their goals:\n")
	if g.CareerTarget.PrimaryGoal != "" {
		sb.WriteString(fmt.Sprintf("- Primary Goal: %s\n", g.CareerTarget.PrimaryGoal))
	}
	if g.LimitsAndTargets.MinCodingHours > 0 {
		sb.WriteString(fmt.Sprintf("- Target: %.1f hours of coding per day\n", g.LimitsAndTargets.MinCodingHours))
	}
	if g.LimitsAndTargets.MinProjectCommits > 0 {
		sb.WriteString(fmt.Sprintf("- Target: %d commits per day\n", g.LimitsAndTargets.MinProjectCommits))
	}
	sb.WriteString("\nWorking Hours & Focus Mode:\n")
	if g.Identity.WorkingHours.StartTime != "" && g.Identity.WorkingHours.EndTime != "" {
		sb.WriteString(fmt.Sprintf("- Expected Working Hours: %s to %s\n", g.Identity.WorkingHours.StartTime, g.Identity.WorkingHours.EndTime))
	}
	if len(g.Identity.WorkingHours.PeakFocusWindows) > 0 {
		sb.WriteString("- Peak Focus Windows (CRITICAL - BE MERCILESS IF DISTRACTED HERE):\n")
		for _, w := range g.Identity.WorkingHours.PeakFocusWindows {
			sb.WriteString(fmt.Sprintf("  * %s to %s\n", w.Start, w.End))
		}
	}
	
	sb.WriteString("\nIf they ask for a roast, be mean, funny, and ruthless about their terrible metrics. Otherwise, act as a strict assistant.\n")
	
	return sb.String()
}


// GetProjectForPath attempts to match a file path to one of the configured projects
func (g *Goals) GetProjectForPath(filePath string) (string, bool) {
	cleanPath := filepath.Clean(strings.ToLower(filePath))
	for _, p := range g.Projects {
		if p.RepositoryPath == "" {
			continue
		}
		repoPath := filepath.Clean(strings.ToLower(p.RepositoryPath))
		if !strings.HasSuffix(repoPath, string(filepath.Separator)) {
			repoPath += string(filepath.Separator)
		}
		
		// If cleanPath is exactly the repo path without trailing slash, it's a match too
		if cleanPath == strings.TrimSuffix(repoPath, string(filepath.Separator)) {
			return p.Name, true
		}

		if strings.HasPrefix(cleanPath, repoPath) {
			return p.Name, true
		}
		for _, alias := range p.DirectoryAliases {
			if alias == "" {
				continue
			}
			// require exact match of directory component to prevent "code" matching "vscode"
			parts := strings.Split(cleanPath, string(filepath.Separator))
			for _, part := range parts {
				if part == strings.ToLower(alias) {
					return p.Name, true
				}
			}
		}
	}
	return "", false
}

// ClassifyApp identifies the category of an application name
func (g *Goals) ClassifyApp(appName string) string {
	lowerApp := strings.ToLower(appName)
	cats := make([]string, 0, len(g.LimitsAndTargets.CategorizationRules.Applications))
	for cat := range g.LimitsAndTargets.CategorizationRules.Applications {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		for _, app := range g.LimitsAndTargets.CategorizationRules.Applications[cat] {
			if app == "" {
				continue
			}
			if strings.Contains(lowerApp, strings.ToLower(app)) {
				return cat
			}
		}
	}
	return "unknown"
}

// ClassifyDomain identifies the category of a web domain or title
func (g *Goals) ClassifyDomain(domainOrTitle string) string {
	lowerDomain := strings.ToLower(domainOrTitle)
	cats := make([]string, 0, len(g.LimitsAndTargets.CategorizationRules.Domains))
	for cat := range g.LimitsAndTargets.CategorizationRules.Domains {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		for _, dom := range g.LimitsAndTargets.CategorizationRules.Domains[cat] {
			if dom == "" {
				continue
			}
			if strings.Contains(lowerDomain, strings.ToLower(dom)) {
				return cat
			}
		}
	}
	return "unknown"
}
