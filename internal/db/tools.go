package db

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Action is something dbk does with a database, each needing its own tools.
type Action int

const (
	ActionTest Action = iota // test connection, list databases
	ActionBackup
	ActionRestore
)

// AllActions is every action, for "can this engine be used at all here?".
var AllActions = []Action{ActionTest, ActionBackup, ActionRestore}

// toolNeeds lists, per engine and action, the client tools required. Each
// inner slice is a set of alternatives: any one of them is enough.
var toolNeeds = map[string]map[Action][][]string{
	"mysql": {
		ActionTest:    {{"mysql", "mariadb"}},
		ActionBackup:  {{"mysqldump", "mariadb-dump"}},
		ActionRestore: {{"mysql", "mariadb"}},
	},
	"postgres": {
		ActionTest:    {{"psql"}},
		ActionBackup:  {{"pg_dump"}},
		ActionRestore: {{"pg_restore"}},
	},
	"mongodb": {
		ActionTest:    {{"mongosh", "mongo"}},
		ActionBackup:  {{"mongodump"}},
		ActionRestore: {{"mongorestore"}},
	},
	"sqlite": {
		ActionTest:    {{"sqlite3"}},
		ActionBackup:  {{"sqlite3"}},
		ActionRestore: {{"sqlite3"}},
	},
}

// MissingToolsError says which client tools an engine needs but this
// computer doesn't have, and how to install them.
type MissingToolsError struct {
	Engine string
	Tools  []string // each entry is one need, e.g. "mysqldump (or mariadb-dump)"
	Hint   string   // install command for this OS, may be empty
}

func (e *MissingToolsError) Error() string {
	msg := fmt.Sprintf("%s client tools are not installed on this computer: missing %s",
		e.Engine, strings.Join(e.Tools, ", "))
	if e.Hint != "" {
		msg += ". Install them with: " + e.Hint
	}
	return msg
}

// CheckTools reports the tools engine needs for actions that aren't installed,
// or nil if all are there (or the engine is unknown).
func CheckTools(engine string, actions ...Action) error {
	needs, ok := toolNeeds[engine]
	if !ok {
		return nil
	}
	if len(actions) == 0 {
		actions = AllActions
	}
	var missing []string
	seen := map[string]bool{}
	for _, a := range actions {
		for _, alts := range needs[a] {
			if _, err := findTool(alts); err == nil {
				continue
			}
			label := alts[0]
			if len(alts) > 1 {
				label += " (or " + strings.Join(alts[1:], ", ") + ")"
			}
			if !seen[label] {
				seen[label] = true
				missing = append(missing, label)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &MissingToolsError{Engine: engine, Tools: missing, Hint: InstallHint(engine)}
}

// engineForTool maps a client tool back to its engine, for error messages.
func engineForTool(tool string) string {
	for engine, needs := range toolNeeds {
		for _, groups := range needs {
			for _, alts := range groups {
				for _, t := range alts {
					if t == tool {
						return engine
					}
				}
			}
		}
	}
	return ""
}

// InstallHint returns the command that installs engine's client tools on this
// computer, or "" when there is no single obvious one.
func InstallHint(engine string) string {
	pm := packageManager()
	hints := map[string]map[string]string{
		"mysql": {
			"brew": "brew install mysql-client (or brew install mariadb for MariaDB servers)",
			"apt":  "sudo apt install default-mysql-client",
			"dnf":  "sudo dnf install mysql",
			"yum":  "sudo yum install mysql",
			"apk":  "apk add mysql-client",
		},
		"postgres": {
			"brew": "brew install libpq",
			"apt":  "sudo apt install postgresql-client",
			"dnf":  "sudo dnf install postgresql",
			"yum":  "sudo yum install postgresql",
			"apk":  "apk add postgresql-client",
		},
		"mongodb": {
			"brew": "brew tap mongodb/brew && brew install mongodb-database-tools mongosh",
		},
		"sqlite": {
			"brew": "brew install sqlite",
			"apt":  "sudo apt install sqlite3",
			"dnf":  "sudo dnf install sqlite",
			"yum":  "sudo yum install sqlite",
			"apk":  "apk add sqlite",
		},
	}
	if h := hints[engine][pm]; h != "" {
		return h
	}
	if engine == "mongodb" {
		return "download MongoDB Database Tools and mongosh from https://www.mongodb.com/try/download/database-tools"
	}
	return ""
}

// packageManager names the package manager to suggest on this computer.
var packageManager = func() string {
	if runtime.GOOS == "darwin" {
		return "brew"
	}
	for _, pm := range []string{"apt", "dnf", "yum", "apk"} {
		if _, err := exec.LookPath(pm); err == nil {
			return pm
		}
	}
	return ""
}
