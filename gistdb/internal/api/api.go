package api

import (
	"encoding/json"
	//"fmt"
	"gistdb-as-a-service/gistdb/internal/dbcache"
	"gistdb-as-a-service/gistdb/internal/githubclient"
	"net/http"
	"strings"
)

type GithubClient interface {
	CreateGist(collection string, content map[string]any) (map[string]any, error)
	GetGistTyped(gistID string) (githubclient.Gist, error)
	ListGists() ([]map[string]any, error)
}

// create handler struct to inject githubclient functions
type Handler struct {
	Client     GithubClient
	DBCache    *dbcache.Cache
	FNameCache *dbcache.Cache
}

func NewHandler(c GithubClient, dbc *dbcache.Cache, fnc *dbcache.Cache) *Handler {
	return &Handler{Client: c, DBCache: dbc, FNameCache: fnc}
}

func (h *Handler) CreateDocumentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	collection := strings.TrimPrefix(r.URL.Path, "/collections/")
	if collection == "" {
		http.Error(w, "missing collection name", http.StatusBadRequest)
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	valueFound := false
	for key := range payload {
		if key == "data" {
			valueFound = true
			break
		}
	}
	if !valueFound {
		http.Error(w, "payload must contain key 'data'", http.StatusBadRequest)
		return
	}

	out, err := h.Client.CreateGist(collection, payload)
	if err != nil {
		http.Error(w, "failed to create document", http.StatusInternalServerError)
		return
	}

	//fmt.Printf("output is woo %v\n\n", out)

	// set to filename cache
	custom_id := out["customId"]
	gist_id := out["id"]

	custom_id_str, ok := custom_id.(string)
	if !ok {
		http.Error(w, "id is not a string", http.StatusInternalServerError)
		return
	}
	h.FNameCache.Set(custom_id_str, gist_id)

	json.NewEncoder(w).Encode(map[string]any{
		"id": custom_id,
	})
}

func (h *Handler) GetDocumentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/documents/")
	if id == "" {
		http.Error(w, "missing document ID", http.StatusBadRequest)
		return
	}

	gist_id, id_found := h.FNameCache.Get(id)
	if !id_found {
		http.Error(w, "id not found", http.StatusNotFound)
		return
	}
	id_str, ok := gist_id.(string)
	if !ok {
		http.Error(w, "id is not a string", http.StatusInternalServerError)
		return
	}

	doc, err := h.Client.GetGistTyped(id_str)
	if err != nil {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}
