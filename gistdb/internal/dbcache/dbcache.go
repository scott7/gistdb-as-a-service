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

	checkWriteFile(c.filepath)

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

func (c *Cache) SetIndexCache(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	checkWriteFile(c.filepath)

	fileCacheContent, err := os.ReadFile(c.filepath)
	if err != nil {
		log.Fatal(err)
	}

	var fileCacheMap map[string]any
	err = json.Unmarshal([]byte(fileCacheContent), &fileCacheMap)
	if err != nil {
		fmt.Println("Error unmarshaling to map:", err)
	}

	existingIds, ok := fileCacheMap[key]
	if !ok {
		return nil
	}

	fmt.Printf("CACHE INDEX: %#v\n", existingIds)

	return nil
}

func (c *Cache) Assign(value map[string]any) error {
	// overwrite cache with specified value
	c.mu.Lock()
	defer c.mu.Unlock()

	checkWriteFile(c.filepath)

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

func (c *Cache) AssignIndexMap(value map[any][]string) error {
	// overwrite cache with specified value for index map value
	c.mu.Lock()
	defer c.mu.Unlock()

	if !fileThere(c.filepath) {
		if err := os.WriteFile(c.filepath, []byte("{}"), 0644); err != nil {
			return fmt.Errorf("error creating cache file: %w", err)
		}
	}

	newData := make(map[string]CacheItem, len(value))

	for key, list := range value {
		keyStr := fmt.Sprintf("%v", key) // convert any → string

		newData[keyStr] = CacheItem{
			Value: list,
		}
	}

	c.data = newData

	// cannot convert non-string keys to json, so serialize here in order to write to file
	// go see's this "map[any][]string" as "map[interface {}][]string" so the key is of type
	// interface which causes it to fail to write.
	serializedIndexMap := make(map[string]any, len(newData))

	for k, item := range newData {
		serializedIndexMap[k] = item.Value
	}

	if err := writeFileCache(serializedIndexMap, c); err != nil {
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
