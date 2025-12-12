package api

import (
	"encoding/json"
	"fmt"
	"gistdb-as-a-service/gistdb/internal/dbcache"
	"gistdb-as-a-service/gistdb/internal/githubclient"
	"net/http"
	"strings"
)

type GithubClient interface {
	CreateGist(collection string, content map[string]any) (map[string]any, error)
	GetGistTyped(gistID string) (githubclient.Gist, error)
	ListGists() ([]map[string]any, error)
	UpdateGist(gistID string, filename string, content map[string]any, collection string) (map[string]any, error)
	DeleteGist(gistID string) (map[string]any, error)
}

// create handler struct to inject githubclient functions
type Handler struct {
	Client     GithubClient
	DBCache    *dbcache.Cache
	FNameCache *dbcache.Cache
	IndexCache *dbcache.Cache
}

func NewHandler(c GithubClient, dbc *dbcache.Cache, fnc *dbcache.Cache, inc *dbcache.Cache) *Handler {
	return &Handler{Client: c, DBCache: dbc, FNameCache: fnc, IndexCache: inc}
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

	// trim trailing "/" in case that is passed in error
	collection = strings.TrimSuffix(collection, "/")

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

	fmt.Printf("output is woo %v\n\n", out)

	// set to filename cache
	custom_id := out["customId"]
	gist_id := out["id"]
	content := out["contentData"]

	custom_id_str, ok := custom_id.(string)
	if !ok {
		http.Error(w, "id is not a string", http.StatusInternalServerError)
		return
	}
	h.FNameCache.Set(custom_id_str, gist_id)
	h.DBCache.Set(custom_id_str, content)

	json.NewEncoder(w).Encode(map[string]any{
		"id": custom_id,
	})
}

func (h *Handler) GetDocumentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/collections/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "expected /collections/{collection}/{id}", http.StatusBadRequest)
		return
	}

	collection := parts[0]
	id := parts[1]

	if collection == "" || id == "" {
		http.Error(w, "missing collection or id", http.StatusBadRequest)
		return
	}

	gist_id, id_found := h.FNameCache.Get(id)
	if !id_found {
		http.Error(w, "id not found", http.StatusNotFound)
		return
	}
	gist_id_str, ok := gist_id.(string)
	if !ok {
		http.Error(w, "id is not a string", http.StatusInternalServerError)
		return
	}

	//cache_doc, ok := h.DBCache.Get(id)
	//if !ok {
	// not found
	//}

	doc, err := h.Client.GetGistTyped(gist_id_str)
	if err != nil {
		http.Error(w, "document not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(doc)
}

func (h *Handler) UpdateDocumentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/collections/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "expected /collections/{collection}/{id}", http.StatusBadRequest)
		return
	}

	collection := parts[0]
	id := parts[1]

	if collection == "" || id == "" {
		http.Error(w, "missing collection or id", http.StatusBadRequest)
		return
	}

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if _, ok := payload["data"]; !ok {
		http.Error(w, "payload must contain key 'data'", http.StatusBadRequest)
		return
	}

	// get gist id
	gistID, idFound := h.FNameCache.Get(id)
	if !idFound {
		http.Error(w, "id not found", http.StatusNotFound)
		return
	}

	gistIDStr, ok := gistID.(string)
	if !ok {
		http.Error(w, "cache value is not a string", http.StatusInternalServerError)
		return
	}

	out, err := h.Client.UpdateGist(gistIDStr, id, payload, collection)
	if err != nil {
		http.Error(w, "unable to update document", http.StatusInternalServerError)
		return
	}
	url := out["url"]
	content := out["contentData"]

	h.DBCache.Set(id, content)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":  id,
		"url": url,
	})
}

func (h *Handler) DeleteDocumentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/collections/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, "expected /collections/{collection}/{id}", http.StatusBadRequest)
		return
	}

	collection := parts[0]
	id := parts[1]

	if collection == "" || id == "" {
		http.Error(w, "missing collection or id", http.StatusBadRequest)
		return
	}

	// get gist id
	gistID, idFound := h.FNameCache.Get(id)
	if !idFound {
		http.Error(w, "id not found", http.StatusNotFound)
		return
	}

	gistIDStr, ok := gistID.(string)
	if !ok {
		http.Error(w, "cache value is not a string", http.StatusInternalServerError)
		return
	}

	doc, err := h.Client.GetGistTyped(gistIDStr)
	if err != nil {
		http.Error(w, "unable to get document", http.StatusInternalServerError)
		return
	}

	collectionFromDoc := doc.Content["collection"]

	if collectionFromDoc != collection {
		http.Error(w, "collection provided does not match document", http.StatusBadRequest)
		return
	}

	_, err_delete := h.Client.DeleteGist(gistIDStr)

	if err_delete != nil {
		http.Error(w, "unable to delete document", http.StatusInternalServerError)
		return
	}
	h.DBCache.Delete(id)
	h.FNameCache.Delete(id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message": "success",
	})
}
