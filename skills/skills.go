package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// exampleSkillContent is the SKILL.md written on first run so users have a
// working reference to learn from. It is intentionally practical, not just a
// placeholder — the git-commit skill is immediately useful.
const exampleSkillContent = `---
name: git-commit
description: Write clear, conventional commit messages. Use when committing code changes.
---

# Git Commit Messages

When writing commit messages, follow these conventions:

## Format
` + "```" + `
<type>(<scope>): <short summary>

[optional body — explain WHY, not what]
[optional footer — Closes #123]
` + "```" + `

## Types
- **feat**: new feature
- **fix**: bug fix
- **docs**: documentation only
- **refactor**: no functional change
- **test**: adding or updating tests
- **chore**: build, deps, tooling

## Rules
1. Summary: 50 chars or fewer, no trailing period
2. Use imperative mood — "add feature" not "added feature"
3. Body: explain the motivation, not the mechanics
4. Reference issues in the footer: ` + "`Closes #42`" + `

## Examples
` + "```" + `
feat(auth): add OAuth2 login via GitHub

Replaces the custom session system. Tokens expire after 24h.
Closes #42
` + "```" + `
` + "```" + `
fix(api): handle empty response from payments service

Empty body on timeout caused a JSON parse panic.
Now returns 503 with a clear error message.
` + "```" + `
`

// Bootstrap creates the example skill if the global skills directory does not
// yet exist. It is safe to call on every startup — it does nothing once the
// directory exists.
func Bootstrap() {
	dir := GlobalDir()
	if _, err := os.Stat(dir); err == nil {
		return // already exists, leave it alone
	}
	skillDir := filepath.Join(dir, "git-commit")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(exampleSkillContent), 0644)
}

// Skill represents a Claude Code skill on the filesystem.
type Skill struct {
	Name       string // directory name (also the skill name)
	Dir        string // absolute path to the skill directory
	FilePath   string // absolute path to SKILL.md inside Dir
	ScopeDir   string // absolute path to the parent skills/ directory
	ScopeLabel string // "Global" or the project name
}

// ProjectRef links a project display name to its primary repo path.
type ProjectRef struct {
	Name        string
	PrimaryRepo string // may contain leading ~
}

// GlobalDir returns the absolute path to ~/.claude/skills.
func GlobalDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "skills")
}

// ProjectDir returns the absolute path to <repo>/.claude/skills.
func ProjectDir(repoPath string) string {
	return filepath.Join(ExpandPath(repoPath), ".claude", "skills")
}

// ExpandPath expands a leading ~ in a path to the user home directory.
func ExpandPath(p string) string {
	if p == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if len(p) >= 2 && p[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// ScanGlobal returns all skills found under ~/.claude/skills/.
func ScanGlobal() []Skill {
	return scanDir(GlobalDir(), "Global")
}

// ScanProject returns all skills found under <repoPath>/.claude/skills/.
func ScanProject(repoPath, projectName string) []Skill {
	return scanDir(ProjectDir(repoPath), projectName)
}

func scanDir(dir, scopeLabel string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, e.Name())
		skillFile := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue // no SKILL.md — not a valid skill
		}
		out = append(out, Skill{
			Name:       e.Name(),
			Dir:        skillDir,
			FilePath:   skillFile,
			ScopeDir:   dir,
			ScopeLabel: scopeLabel,
		})
	}
	return out
}

// Create creates a new skill at <scopeDir>/<name>/SKILL.md with a starter template.
// Returns the absolute path to the created SKILL.md.
func Create(scopeDir, name string) (string, error) {
	skillDir := filepath.Join(scopeDir, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return "", fmt.Errorf("could not create skill directory: %w", err)
	}
	filePath := filepath.Join(skillDir, "SKILL.md")
	template := fmt.Sprintf("---\nname: %s\ndescription: \n---\n\n# %s\n\nDescribe what this skill does and any instructions for Claude.\n", name, name)
	if err := os.WriteFile(filePath, []byte(template), 0644); err != nil {
		return "", fmt.Errorf("could not write SKILL.md: %w", err)
	}
	return filePath, nil
}

// Delete removes a skill directory entirely (the directory that contains SKILL.md).
func Delete(skillDir string) error {
	return os.RemoveAll(skillDir)
}

// ReadContent reads and returns the contents of a SKILL.md file.
// Returns an empty string if the file cannot be read.
func ReadContent(filePath string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return string(data)
}
