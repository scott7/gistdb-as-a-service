package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

import (
	"gistdb-as-a-service/gistdb/internal/api"
	"gistdb-as-a-service/gistdb/internal/auth"
	"gistdb-as-a-service/gistdb/internal/dbcache"
	"gistdb-as-a-service/gistdb/internal/githubclient"
)

func main() {
	err := auth.InitJWT()
	if err != nil {
		log.Fatalf("JWT init failed: %v", err)
	}

	cache := dbcache.NewCache("/tmp/gocache.json", 3600) // items expire in 1 hour
	filename_cache := dbcache.NewCache("/tmp/fnamecache.json", 0)
	index_cache := dbcache.NewCache("/tmp/index.json", 0)
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is required")
	}

	client := githubclient.NewGitHubClient(token)

	gists, err := client.ListGists()
	if err != nil {
		log.Fatal(err)
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

	//fmt.Printf("indexMap: %#v\n", indexMap)

	index_cache.AssignIndexMap(indexMap)

	// Initialize API

	handler := api.NewHandler(client, cache, filename_cache, index_cache)

	mux := http.NewServeMux()

	mux.HandleFunc("/collections/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handler.GetDocumentHandler(w, r)
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
	log.Fatal(http.ListenAndServe(":8080", auth.Middleware(mux)))

}
