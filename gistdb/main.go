package main

import (
	"fmt"
	"log"
	"os"
)

import (
	"gistdb-as-a-service/gistdb/internal/dbcache"
	"gistdb-as-a-service/gistdb/internal/githubclient"
)

func main() {
	cache := dbcache.NewCache("/tmp/gocache.json")
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
	for _, gistName := range gists_map {
		fmt.Printf("gist name: %#v\n", gistName)
	}

	for gistID := range gists_map {
		gistRes, err := client.GetGistTyped(gistID)
		if err != nil {
			continue
		}
		fmt.Printf("Setting gist in cache: %#v\n", gistRes.Content)
		cache.Set(gistRes.Name, gistRes.Content)
	}
	cache.Get("test_2")

	fmt.Printf("====\n")
}
