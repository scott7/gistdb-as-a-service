package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gistdb-as-a-service/gistdb/internal/api"
	"gistdb-as-a-service/gistdb/internal/auth"
	"gistdb-as-a-service/gistdb/internal/dbcache"
	"gistdb-as-a-service/gistdb/internal/githubclient"
)

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func populateCaches(client api.GithubClient, cache, filename_cache, index_cache *dbcache.Cache) error {
	gists, err := client.ListGists()
	if err != nil {
		return err
	}

	gists_map := githubclient.ExtractGistNames(gists)
	fmt.Println(gists_map)
	filename_cache.Assign(gists_map)
	for _, gistName := range gists_map {
		fmt.Printf("gist name: %#v\n", gistName)
	}

	indexMap := make(map[any][]string)

	for _, gistID := range gists_map {
		gistIdStr, _ := gistID.(string)
		gistRes, err := client.GetGistTyped(gistIdStr)
		if err != nil {
			continue
		}
		cache.Set(gistRes.Name, gistRes.Content)
		collection, ok := gistRes.Content["collection"]
		if ok {
			indexMap[collection] = append(indexMap[collection], gistRes.Name)
		}
	}

	index_cache.AssignIndexMap(indexMap)

	return nil
}

func main() {
	err := auth.InitJWT()
	if err != nil {
		log.Fatalf("JWT init failed: %v", err)
	}

	// cache items expire in 1 hour, if set to 0 never expire
	cache := dbcache.NewCache("/tmp/gocache.json", 3600)
	filename_cache := dbcache.NewCache("/tmp/fnamecache.json", 0)
	index_cache := dbcache.NewCache("/tmp/index.json", 0)
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}

	client := githubclient.NewGitHubClient(token)

	if err := populateCaches(client, cache, filename_cache, index_cache); err != nil {
		log.Fatal(err)
	}

	// background refresh cache every 15 minutes (instead of relying on restart)
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if err := populateCaches(client, cache, filename_cache, index_cache); err != nil {
				log.Printf("cache refresh failed: %v", err)
			}
		}
	}()

	handler := api.NewHandler(client, cache, filename_cache, index_cache)

	mux := http.NewServeMux()

	mux.HandleFunc("/collections_list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handler.ListCollectionsHandler(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/clear_cache", func(w http.ResponseWriter, r *http.Request) {
		handler.ClearCacheHandler(w, r)
	})

	mux.HandleFunc("/collections/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// GET /collections/{collection} vs GET /collections/{collection}/{id}
			path := strings.TrimPrefix(r.URL.Path, "/collections/")
			parts := strings.SplitN(strings.TrimSuffix(path, "/"), "/", 2)
			if len(parts) == 2 && parts[1] != "" {
				handler.GetDocumentHandler(w, r)
			} else {
				handler.ListCollectionHandler(w, r)
			}
		case http.MethodPost:
			handler.CreateDocumentHandler(w, r)
		case http.MethodPatch:
			handler.UpdateDocumentHandler(w, r)
		case http.MethodDelete:
			handler.DeleteDocumentHandler(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	log.Println("Server running on :8080")
	log.Fatal(http.ListenAndServe(":8080", corsMiddleware(auth.Middleware(mux))))

}
