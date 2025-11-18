package githubclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Gist struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Content map[string]any `json:"content"`
}

type GitHubClient struct {
	httpClient *http.Client
	token      string
}

func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		httpClient: &http.Client{},
		token:      token,
	}
}

type gistAPIResponse struct {
	ID    string `json:"id"`
	Files map[string]struct {
		Filename string `json:"filename"`
		Content  any    `json:"content"`
	} `json:"files"`
}

func (c *GitHubClient) doGitHubRequest(method, url string, body io.Reader, out any) error {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return err
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned status: %d", res.StatusCode)
	}

	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return err
		}
	}

	return nil
}

func (c *GitHubClient) ListGists() ([]map[string]any, error) {
	url := "https://api.github.com/gists"
	var out []map[string]any
	if err := c.doGitHubRequest("GET", url, nil, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func (c *GitHubClient) GetGist(gistID string) (map[string]any, error) {
	url := "https://api.github.com/gists/" + gistID

	var out map[string]any
	if err := c.doGitHubRequest("GET", url, nil, &out); err != nil {
		return nil, err
	}

	return out, nil
}

func (c *GitHubClient) GetGistTyped(gistID string) (Gist, error) {
	raw, err := c.GetGist(gistID)
	if err != nil {
		return Gist{}, err
	}

	api, err := parseGist(raw)
	if err != nil {
		return Gist{}, err
	}

	gist, err := ConvertToGist(api)
	if err != nil {
		return Gist{}, err
	}

	return gist, nil
}

func ExtractGistNames(gists []map[string]any) map[string]string {
	result := make(map[string]string)
	for _, g := range gists {
		id, ok := g["id"].(string)
		if !ok {
			continue
		}

		filesRaw, ok := g["files"].(map[string]any)
		if !ok {
			continue
		}

		for filename := range filesRaw {
			result[id] = filename
			break
		}
	}

	return result
}

func parseGist(raw map[string]any) (gistAPIResponse, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return gistAPIResponse{}, err
	}

	var api gistAPIResponse
	if err := json.Unmarshal(data, &api); err != nil {
		return gistAPIResponse{}, err
	}

	return api, nil
}

func ConvertToGist(api gistAPIResponse) (Gist, error) {
	g := Gist{
		ID:      api.ID,
		Content: make(map[string]any),
	}

	for filename, f := range api.Files {
		g.Name = filename

		contentStr, ok := f.Content.(string)
		if !ok {
			return g, fmt.Errorf("content is not a string")
		}

		// unmarshal JSON string into map
		var contentMap map[string]any
		if err := json.Unmarshal([]byte(contentStr), &contentMap); err != nil {
			return g, fmt.Errorf("error unmarshaling content: %w", err)
		}

		g.Content = contentMap
		break
	}

	return g, nil
}
