package dbcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
)

type CacheItem struct {
	Value any
}

type Cache struct {
	data     map[string]CacheItem
	mu       sync.RWMutex
	filepath string
}

func fileThere(filePath string) bool {
	_, err := os.Stat(filePath)
	return !errors.Is(err, os.ErrNotExist)
}

func writeFileCache(fileCacheMap map[string]any, cache *Cache) error {
	// Serialize entire cache to JSON
	bytes, err := json.MarshalIndent(fileCacheMap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode cache: %w", err)
	}

	// Write JSON to file atomically
	tmp := cache.filepath + ".tmp"

	if err := os.WriteFile(tmp, bytes, 0644); err != nil {
		return fmt.Errorf("failed to write tmp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmp, cache.filepath); err != nil {
		return fmt.Errorf("failed to replace cache file: %w", err)
	}
	return nil
}

// initialize a new Cache instance
func NewCache(path string) *Cache {
	return &Cache{
		data:     make(map[string]CacheItem),
		filepath: path,
	}
}

func (c *Cache) Set(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// clear in memory cache if we exceed a large number of items
	length := len(c.data)
	if length > 1000 {
		c.data = make(map[string]CacheItem)
	}

	// set in memory cache
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

	//Unmarshal into a generic map
	var fileCacheMap map[string]any
	err = json.Unmarshal([]byte(fileCacheContent), &fileCacheMap)
	if err != nil {
		fmt.Println("Error unmarshaling to map:", err)
	}
	fileCacheMap[key] = value

	if err := writeFileCache(fileCacheMap, c); err != nil {
		return err
	}

	return nil

}

func (c *Cache) Assign(value map[string]any) error {
	// overwrite cache with specified value
	c.mu.Lock()
	defer c.mu.Unlock()

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

	newData := make(map[string]CacheItem, len(value))

	for key, val := range value {
		newData[key] = CacheItem{
			Value: val,
		}
	}

	c.data = newData

	if err := writeFileCache(value, c); err != nil {
		return err
	}

	return nil
}

func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// check file cache if not found in memory
	item, ok := c.data[key]
	if !ok {
		fmt.Println("not found in memory - checking file cache")
		fileCacheContent, err := os.ReadFile(c.filepath)
		if err != nil {
			return nil, false
		}
		var fileCacheMap map[string]any
		err = json.Unmarshal([]byte(fileCacheContent), &fileCacheMap)
		if err != nil {
			fmt.Println("Error unmarshaling to map:", err)
		}
		item, fileOk := fileCacheMap[key]
		if !fileOk {
			fmt.Printf("not found in file cache\n")
			return nil, false
		}
		fmt.Printf("found in file cache: %v\n", item)
		return item, true
	}
	//fmt.Printf("found in file in memory cache: %v\n", item)
	return item.Value, true
}

func (c *Cache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)

	if !fileThere(c.filepath) {
		fmt.Printf("File '%s' does not exist.\n", c.filepath)
		return nil
	}

	fileCacheContent, err := os.ReadFile(c.filepath)
	if err != nil {
		log.Fatal(err)
	}

	//Unmarshal into a generic map
	var fileCacheMap map[string]any
	err = json.Unmarshal([]byte(fileCacheContent), &fileCacheMap)
	if err != nil {
		return fmt.Errorf("Error unmarshaling to map: %w", err)
	}

	delete(fileCacheMap, key)

	if err := writeFileCache(fileCacheMap, c); err != nil {
		return err
	}

	return nil

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
