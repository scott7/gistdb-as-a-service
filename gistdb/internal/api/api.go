package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

import (
	"gistdb-as-a-service/gistdb/internal/common"
	"gistdb-as-a-service/gistdb/internal/dbcache"
)

type GithubClient interface {
	CreateGist(collection string, content map[string]any) (map[string]any, error)
	GetGistTyped(gistID string) (common.Gist, error)
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

func (h *Handler) ListCollectionsHandler(w http.ResponseWriter, r *http.Request) {
	collections := h.IndexCache.Keys()
	sort.Strings(collections)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"collections": collections})
}

func (h *Handler) ListCollectionHandler(w http.ResponseWriter, r *http.Request) {
	collection := strings.TrimPrefix(r.URL.Path, "/collections/")
	collection = strings.TrimSuffix(collection, "/")
	if collection == "" {
		http.Error(w, "missing collection name", http.StatusBadRequest)
		return
	}

	ids, _ := h.IndexCache.GetStrings(collection)
	if ids == nil {
		ids = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ids": ids})
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

	// update index cache so the new document appears
	h.IndexCache.AppendToList(collection, custom_id_str)

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

	var doc common.Gist
	var err error

	cache_doc, ok := h.DBCache.Get(id)
	if ok {
		// return cached content without touching github
		cache_doc_converted, ok := cache_doc.(map[string]any)
		if !ok {
			http.Error(w, "error getting doc from cache", http.StatusInternalServerError)
			return
		}
		// convert cache doc to Gist format type
		doc = common.Gist{
			GistID:  gist_id_str,
			Content: make(map[string]any),
		}
		doc.Content = cache_doc_converted
		doc.Name = id
		fmt.Println("getting from cache doc: ", gist_id_str)
	} else {
		// cache miss - reach out to github and get document
		doc, err = h.Client.GetGistTyped(gist_id_str)
		fmt.Println("getting from github: ", gist_id_str)
		if err != nil {
			http.Error(w, "document not found", http.StatusNotFound)
			return
		}
		h.DBCache.Set(id, doc.Content)
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

	collectionFromDoc, ok := doc.Content["collection"].(string)
	if !ok || collectionFromDoc != collection {
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

	// remove id from index cache
	h.IndexCache.RemoveFromList(collection, id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message": "success",
	})
}

func (h *Handler) ClearCacheHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	h.DBCache.Clear()
	h.DBCache.ClearFile()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"message": "success",
	})
}
