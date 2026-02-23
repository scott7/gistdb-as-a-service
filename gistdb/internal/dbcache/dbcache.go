package dbcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// ttl for cache is set per Cache object created.
// A ttl value of 0 means there is no expiration.

type CacheItem struct {
	Value  any
	Expiry time.Time
}

type Cache struct {
	data     map[string]CacheItem
	mu       sync.RWMutex
	filepath string
	ttl      int
}

func fileThere(filePath string) bool {
	_, err := os.Stat(filePath)
	return !errors.Is(err, os.ErrNotExist)
}

func checkWriteFile(filepath string) {
	// set data in file based memory cache
	// create file if it does not exist
	if fileThere(filepath) {
		fmt.Printf("File '%s' exists.\n", filepath)
	} else {
		err := os.WriteFile(filepath, []byte("{}"), 0644)
		if err != nil {
			log.Fatalf("Error writing to file: %v", err)
		}
	}
}

func writeFileCache(fileCacheMap any, cache *Cache) error {
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
func NewCache(path string, ttl_value int) *Cache {
	return &Cache{
		data:     make(map[string]CacheItem),
		filepath: path,
		ttl:      ttl_value,
	}
}

func (c *Cache) Set(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// clear in memory cache if we exceed a large number of items
	length := len(c.data)
	// If cache exceeds 1 GB clear it
	if length > 1073741824 {
		c.data = make(map[string]CacheItem)
	}

	var expirationTime time.Time
	if c.ttl == 0 {
		expirationTime = time.Time{}
	} else {
		expirationTime = time.Now().Add(time.Duration(c.ttl) * time.Second)
	}

	// set in memory cache
	c.data[key] = CacheItem{
		Value:  value,
		Expiry: expirationTime,
	}

	checkWriteFile(c.filepath)

	fileCacheContent, err := os.ReadFile(c.filepath)
	if err != nil {
		log.Fatal(err)
	}

	//Unmarshal into a map of CacheItem
	var fileCacheMap map[string]CacheItem
	err = json.Unmarshal([]byte(fileCacheContent), &fileCacheMap)
	if err != nil {
		fmt.Println("Error unmarshaling to map:", err)
	}

	fileCacheMap[key] = CacheItem{
		Value:  value,
		Expiry: expirationTime,
	}

	if err := writeFileCache(fileCacheMap, c); err != nil {
		return err
	}

	return nil

}

func (c *Cache) Assign(value map[string]any) error {
	// overwrite cache with specified value
	c.mu.Lock()
	defer c.mu.Unlock()

	checkWriteFile(c.filepath)

	newData := make(map[string]CacheItem, len(value))

	var expirationTime time.Time
	if c.ttl == 0 {
		expirationTime = time.Time{}
	} else {
		expirationTime = time.Now().Add(time.Duration(c.ttl) * time.Second)
	}

	for key, val := range value {
		newData[key] = CacheItem{
			Value:  val,
			Expiry: expirationTime,
		}
	}

	c.data = newData

	if err := writeFileCache(newData, c); err != nil {
		return err
	}

	return nil
}

func (c *Cache) AssignIndexMap(value map[any][]string) error {
	// overwrite cache with specified value for index map value
	c.mu.Lock()
	defer c.mu.Unlock()

	if !fileThere(c.filepath) {
		if err := os.WriteFile(c.filepath, []byte("{}"), 0644); err != nil {
			return fmt.Errorf("error creating cache file: %w", err)
		}
	}

	var expirationTime time.Time
	if c.ttl == 0 {
		expirationTime = time.Time{}
	} else {
		expirationTime = time.Now().Add(time.Duration(c.ttl) * time.Second)
	}

	newData := make(map[string]CacheItem, len(value))

	for key, list := range value {
		keyStr := fmt.Sprintf("%v", key) // convert any to string

		newData[keyStr] = CacheItem{
			Value:  list,
			Expiry: expirationTime,
		}
	}

	c.data = newData

	if err := writeFileCache(newData, c); err != nil {
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
		var fileCacheMap map[string]CacheItem
		err = json.Unmarshal([]byte(fileCacheContent), &fileCacheMap)
		if err != nil {
			fmt.Println("Error unmarshaling to map:", err)
			return nil, false
		}
		cacheItem, fileOk := fileCacheMap[key]
		if !fileOk {
			fmt.Printf("not found in file cache\n")
			return nil, false
		}
		fmt.Printf("found in file cache: %v\n", cacheItem)

		// Check if item from file cache is expired
		if !cacheItem.Expiry.IsZero() && cacheItem.Expiry.Before(time.Now()) {
			fmt.Printf("item from file cache is expired\n")
			c.deleteUnlocked(key)
			return nil, false
		}

		// Add to in-memory cache and return from there
		c.data[key] = cacheItem
		item = cacheItem
	}
	if !item.Expiry.IsZero() && item.Expiry.Before(time.Now()) {
		c.deleteUnlocked(key)
		return nil, false
	}
	//fmt.Printf("found in file in memory cache: %v\n", item)
	return item.Value, true
}

func (c *Cache) GetStrings(key string) ([]string, bool) {
	// GetStrings retrieves a value as []string, handling the []interface{} that
	// json.Unmarshal produces when deserializing from the file cache.
	// used for IndexCache
	val, ok := c.Get(key)
	if !ok {
		return nil, false
	}
	switch v := val.(type) {
	case []string:
		return v, true
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func (c *Cache) deleteUnlocked(key string) error {
	// helper functino to delete cache - assumes mutex is unlocked.
	fmt.Printf("removing item from cache\n")
	delete(c.data, key)

	if !fileThere(c.filepath) {
		fmt.Printf("File '%s' does not exist.\n", c.filepath)
		return nil
	}

	fileCacheContent, err := os.ReadFile(c.filepath)
	if err != nil {
		log.Fatal(err)
	}

	//Unmarshal into a map of CacheItem
	var fileCacheMap map[string]CacheItem
	err = json.Unmarshal([]byte(fileCacheContent), &fileCacheMap)
	if err != nil {
		return fmt.Errorf("error unmarshaling to map: %w", err)
	}

	delete(fileCacheMap, key)

	if err := writeFileCache(fileCacheMap, c); err != nil {
		return err
	}

	return nil
}

func (c *Cache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deleteUnlocked(key)
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
