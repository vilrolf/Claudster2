package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	StatusUnstarted  = "unstarted"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
)

type Todo struct {
	ID          string    `yaml:"id"`
	Title       string    `yaml:"title"`
	Description string    `yaml:"description,omitempty"`
	Source      string    `yaml:"source"` // "manual" or "jira"
	JiraKey     string    `yaml:"jira_key,omitempty"`
	JiraProject string    `yaml:"jira_project,omitempty"`
	Status      string    `yaml:"status,omitempty"` // unstarted, in_progress, done
	GroupName   string    `yaml:"group_name,omitempty"`
	ProjectName string    `yaml:"project_name,omitempty"`
	SessionName string    `yaml:"session_name,omitempty"`
	CreatedAt   time.Time `yaml:"created_at"`
}

type TodoList struct {
	Todos []Todo `yaml:"todos"`
}

func TodosPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claudster-todos.yaml")
}

func LoadTodos() (TodoList, error) {
	data, err := os.ReadFile(TodosPath())
	if err != nil {
		if os.IsNotExist(err) {
			return TodoList{}, nil
		}
		return TodoList{}, err
	}
	var tl TodoList
	if err := yaml.Unmarshal(data, &tl); err != nil {
		return TodoList{}, err
	}
	return tl, nil
}

func SaveTodos(tl TodoList) error {
	data, err := yaml.Marshal(tl)
	if err != nil {
		return err
	}
	return os.WriteFile(TodosPath(), data, 0644)
}

func NewTodoID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// MergeJiraTodos upserts Jira-sourced todos by JiraKey, leaving manual todos untouched.
func (tl *TodoList) MergeJiraTodos(incoming []Todo) {
	byKey := make(map[string]int, len(tl.Todos))
	for i, t := range tl.Todos {
		if t.JiraKey != "" {
			byKey[t.JiraKey] = i
		}
	}
	for _, t := range incoming {
		if i, exists := byKey[t.JiraKey]; exists {
			tl.Todos[i].Title = t.Title
			tl.Todos[i].Description = t.Description
		} else {
			tl.Todos = append(tl.Todos, t)
		}
	}
}
