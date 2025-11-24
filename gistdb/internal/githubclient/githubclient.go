package githubclient

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Gist struct {
	GistID  string         `json:"gist_id"`
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

func NewShortID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b) + ".json"
}

func getTime() string {
	currentTime := time.Now()
	formattedTime := currentTime.Format(time.RFC3339)
	return formattedTime
}

type gistAPIResponse struct {
	GistID string `json:"id"`
	Files  map[string]struct {
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

	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
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
			result[filename] = id
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
		GistID:  api.GistID,
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

func (c *GitHubClient) UpdateGist(gistID string, filename string, content map[string]any, gists_map map[string]string) (map[string]any, error) {
	url := "https://api.github.com/gists/" + gistID
	var out map[string]any
	jsonBytes, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return nil, err
	}
	filename_from_map := gists_map[gistID]
	if filename != filename_from_map {
		return nil, fmt.Errorf("provided filename does not match recorded filename in database: %v", filename)
	}

	jsonString := string(jsonBytes)
	payload := map[string]any{
		"files": map[string]any{
			filename: map[string]any{
				"content": jsonString,
			},
		},
	}
	jsonPayload, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("error marshalling content: %w", err)
	}
	bodyReader := bytes.NewReader(jsonPayload)
	if err := c.doGitHubRequest("PATCH", url, bodyReader, &out); err != nil {
		fmt.Printf("error here\n")
		return nil, err
	}

	return out, nil
}

func (c *GitHubClient) CreateGist(collection string, content map[string]any) (map[string]any, error) {
	url := "https://api.github.com/gists"
	var out map[string]any
	filename := NewShortID()
	content["id"] = filename
	content["collection"] = collection
	content["createdAt"] = getTime()
	jsonBytes, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return nil, err
	}

	jsonString := string(jsonBytes)
	payload := map[string]any{
		"public": false,
		"files": map[string]any{
			filename: map[string]any{
				"content": jsonString,
			},
		},
	}
	jsonPayload, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("error marshalling content: %w", err)
	}
	bodyReader := bytes.NewReader(jsonPayload)
	if err := c.doGitHubRequest("POST", url, bodyReader, &out); err != nil {
		return nil, err
	}
	out["customId"] = filename

	return out, nil
}

func (c *GitHubClient) DeleteGist(gistID string) (map[string]any, error) {
	url := "https://api.github.com/gists/" + gistID

	var out map[string]any
	if err := c.doGitHubRequest("DELETE", url, nil, &out); err != nil {
		return nil, err
	}

	return out, nil
}
