package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

// cache

func fileThere(filePath string) bool {
	_, err := os.Stat(filePath)
	return !errors.Is(err, os.ErrNotExist)
}

type CacheItem struct {
	Value any
}

type Cache struct {
	data     map[string]CacheItem
	mu       sync.RWMutex
	filepath string
}

// NewCache creates and initializes a new Cache instance.
func NewCache(path string) *Cache {
	return &Cache{
		data:     make(map[string]CacheItem),
		filepath: path,
	}
}

func (c *Cache) Set(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	fmt.Printf("key is %#v\n", key)
	fmt.Printf("value is %#v\n\n", value)

	// set im memory cache
	c.data[key] = CacheItem{
		Value: value,
	}

	// set data in file based memory cache
	// create file if it does not exist
	if fileThere(c.filepath) {
		fmt.Printf("File '%s' exists.\n", c.filepath)
	} else {
		err := os.WriteFile(c.filepath, []byte("{}"), 0644)
		if err != nil {
			log.Fatalf("Error writing to file: %v", err)
		}
	}

	fileCacheContent, err := os.ReadFile(c.filepath)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("============")
	fmt.Println(string(fileCacheContent))
	fmt.Println("============")

	//Unmarshal into a generic map
	var fileCacheMap map[string]any
	err = json.Unmarshal([]byte(fileCacheContent), &fileCacheMap)
	if err != nil {
		fmt.Println("Error unmarshaling to map:", err)
	}
	fileCacheMap[key] = value
	fmt.Printf("Unmarshal to map: %+v\n", fileCacheMap)

	// Serialize entire cache to JSON
	bytes, err := json.MarshalIndent(fileCacheMap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode cache: %w", err)
	}

	// Write JSON to file atomically
	tmp := c.filepath + ".tmp"

	if err := os.WriteFile(tmp, bytes, 0644); err != nil {
		return fmt.Errorf("failed to write tmp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmp, c.filepath); err != nil {
		return fmt.Errorf("failed to replace cache file: %w", err)
	}

	return nil

}

func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// TODO checks file cache if this fails
	item, ok := c.data[key]
	if !ok {
		return nil, false
	}
	return item.Value, true
}

func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]CacheItem)
}

func (c *Cache) ClearFile() {
	c.mu.Lock()
	defer c.mu.Unlock()
	os.Remove(c.filepath)
}

// end cache

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

func ExtractGistURLs(gists []map[string]any) map[string]string {
	result := make(map[string]string)

	for _, g := range gists {
		id, ok := g["id"].(string)
		if !ok {
			continue
		}

		url, ok := g["html_url"].(string)
		if !ok {
			continue
		}

		result[id] = url
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

func main() {
	cache := NewCache("/tmp/gocache.json")
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}

	client := NewGitHubClient(token)

	gists, err := client.ListGists()
	if err != nil {
		log.Fatal(err)
	}

	gists_map := ExtractGistURLs(gists)
	fmt.Println(gists_map)

	for gistID, _ := range gists_map {
		gistRes, err := client.GetGistTyped(gistID)
		if err != nil {
			continue
		}
		fmt.Printf("Setting gist in cache: %#v\n", gistRes.Content)
		cache.Set(gistRes.Name, gistRes.Content)
	}

	fmt.Printf("====\n")
}
