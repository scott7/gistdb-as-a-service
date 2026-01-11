package common

type Gist struct {
	GistID  string         `json:"gist_id"`
	Name    string         `json:"name"`
	Content map[string]any `json:"content"`
}
