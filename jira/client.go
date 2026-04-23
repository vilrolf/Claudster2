package jira

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"claudster/store"
)

type searchResponse struct {
	Total  int `json:"total"`
	Issues []struct {
		Key    string `json:"key"`
		Fields struct {
			Summary     string      `json:"summary"`
			Description interface{} `json:"description"`
			Labels      []string    `json:"labels"`
			Project     struct {
				Key string `json:"key"`
			} `json:"project"`
		} `json:"fields"`
	} `json:"issues"`
}

// FetchAssigned returns open Jira issues assigned to the current user for the configured projects.
func FetchAssigned(cfg store.JiraConfig) ([]store.Todo, int, error) {
	if cfg.URL == "" || cfg.Email == "" || cfg.APIToken == "" {
		return nil, 0, nil
	}

	jql := fmt.Sprintf("assignee = \"%s\" AND statusCategory != Done", cfg.Email)
	if len(cfg.Projects) > 0 {
		jql += fmt.Sprintf(" AND project in (%s)", strings.Join(cfg.Projects, ","))
	}
	jql += " ORDER BY updated DESC"

	reqURL := fmt.Sprintf("%s/rest/api/3/search/jql", strings.TrimRight(cfg.URL, "/"))

	body, _ := json.Marshal(map[string]interface{}{
		"jql":        jql,
		"maxResults": 50,
		"fields":     []string{"summary", "description", "project", "labels"},
	})

	req, err := http.NewRequest("POST", reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(cfg.Email + ":" + cfg.APIToken))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("jira returned HTTP %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, 0, err
	}

	todos := make([]store.Todo, 0, len(sr.Issues))
	for _, i := range sr.Issues {
		todos = append(todos, store.Todo{
			ID:          store.NewTodoID(),
			Title:       i.Fields.Summary,
			Description: extractDescription(i.Fields.Description),
			Labels:      i.Fields.Labels,
			Source:      "jira",
			JiraKey:     i.Key,
			JiraProject: i.Fields.Project.Key,
			CreatedAt:   time.Now(),
		})
	}
	return todos, sr.Total, nil
}

func extractDescription(raw interface{}) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case map[string]interface{}:
		return extractADFText(v)
	}
	return ""
}

func extractADFText(node map[string]interface{}) string {
	if text, ok := node["text"].(string); ok {
		return text
	}
	content, ok := node["content"].([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, c := range content {
		if child, ok := c.(map[string]interface{}); ok {
			if t := extractADFText(child); t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, " ")
}
