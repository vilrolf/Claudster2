package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Session struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind,omitempty"` // "": claude (default), "editor", "lazygit"
}

func (s Session) IsToolSession() bool {
	return s.Kind == "editor" || s.Kind == "lazygit" || s.Kind == "terminal"
}

type Project struct {
	Name     string    `yaml:"name"`
	Repos    []string  `yaml:"repos"`
	Sessions []Session `yaml:"sessions"`
}

func (p *Project) PrimaryRepo() string {
	if len(p.Repos) == 0 {
		return "~"
	}
	return p.Repos[0]
}

type Group struct {
	Name     string    `yaml:"name"`
	Projects []Project `yaml:"projects"`
}

type UI struct {
	SidebarWidth int `yaml:"sidebar_width,omitempty"`
}

type Config struct {
	Groups []Group `yaml:"groups"`
	UI     UI      `yaml:"ui,omitempty"`
}

func ConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claudster.yaml")
}

func Load() (Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return c, nil
}

func Save(c Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), data, 0644)
}

func (c *Config) AddProject(groupName string, p Project) {
	for i := range c.Groups {
		if c.Groups[i].Name == groupName {
			c.Groups[i].Projects = append(c.Groups[i].Projects, p)
			return
		}
	}
	c.Groups = append(c.Groups, Group{Name: groupName, Projects: []Project{p}})
}

func (c *Config) AddSession(groupName, projectName string, s Session) {
	for gi := range c.Groups {
		if c.Groups[gi].Name == groupName {
			for pi := range c.Groups[gi].Projects {
				if c.Groups[gi].Projects[pi].Name == projectName {
					c.Groups[gi].Projects[pi].Sessions = append(
						c.Groups[gi].Projects[pi].Sessions, s)
					return
				}
			}
		}
	}
}

// InsertProjectTemplate adds a placeholder project to the given group, saves
// the file, and returns the 1-indexed line number of the placeholder so the
// editor can open at exactly that spot.
func InsertProjectTemplate(groupName string) (int, error) {
	cfg, err := Load()
	if err != nil {
		return 0, err
	}
	cfg.AddProject(groupName, Project{
		Name:  "project-name",
		Repos: []string{"~/code/project"},
	})
	if err := Save(cfg); err != nil {
		return 0, err
	}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return 0, err
	}
	for i, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "project-name") {
			return i + 1, nil
		}
	}
	return 1, nil
}

func (c *Config) RemoveSession(groupName, projectName, sessionName string) {
	for gi := range c.Groups {
		if c.Groups[gi].Name == groupName {
			for pi := range c.Groups[gi].Projects {
				if c.Groups[gi].Projects[pi].Name == projectName {
					ss := c.Groups[gi].Projects[pi].Sessions
					out := ss[:0]
					for _, s := range ss {
						if s.Name != sessionName {
							out = append(out, s)
						}
					}
					c.Groups[gi].Projects[pi].Sessions = out
					return
				}
			}
		}
	}
}
