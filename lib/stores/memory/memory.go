package memory

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/deroproject/graviton"
	"github.com/fxamacker/cbor/v2"
	"github.com/nbd-wtf/go-nostr"

	stores "github.com/HORNET-Storage/hornet-storage/lib/stores"
	merkle_dag "github.com/HORNET-Storage/scionic-merkletree/dag"

	jsoniter "github.com/json-iterator/go"

	types "github.com/HORNET-Storage/hornet-storage/lib"
)

const (
	AddressStatusAvailable = "available"
	AddressStatusAllocated = "allocated"
	AddressStatusUsed      = "used"
)

type GravitonMemoryStore struct {
	Database *graviton.Store

	CacheConfig map[string]string
}

func (store *GravitonMemoryStore) InitStore(basepath string, args ...interface{}) error {
	db, err := graviton.NewMemStore()
	if err != nil {
		return err
	}

	store.Database = db

	snapshot, err := db.LoadSnapshot(0)
	if err != nil {
		return err
	}

	tree, err := snapshot.GetTree("content")
	if err != nil {
		return err
	}

	_, err = graviton.Commit(tree)
	if err != nil {
		return err
	}

	store.CacheConfig = map[string]string{}
	for _, arg := range args {
		if cacheConfig, ok := arg.(map[string]string); ok {
			store.CacheConfig = cacheConfig
		}
	}

	return nil
}

// Not implemented for the Memory Store
func (store *GravitonMemoryStore) GetStatsStore() stores.StatisticsStore {
	return nil
}

func (store *GravitonMemoryStore) QueryDag(filter map[string]string) ([]string, error) {
	keys := []string{}

	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return nil, err
	}

	for bucket, key := range filter {
		if _, ok := store.CacheConfig[bucket]; ok {
			cacheTree, err := snapshot.GetTree(bucket)
			if err == nil {
				if strings.HasPrefix(bucket, "npub") {
					value, err := cacheTree.Get([]byte(bucket))
					if err == nil {
						var cacheData *types.CacheData = &types.CacheData{}

						err = cbor.Unmarshal(value, cacheData)
						if err == nil {
							keys = append(keys, cacheData.Keys...)
						}
					}
				} else {
					value, err := cacheTree.Get([]byte(key))
					if err == nil {
						keys = append(keys, string(value))
					}
				}
			}
		}
	}

	return keys, nil
}

func (store *GravitonMemoryStore) StoreLeaf(root string, leafData *types.DagLeafData) error {
	if leafData.Leaf.ContentHash != nil && leafData.Leaf.Content == nil {
		return fmt.Errorf("leaf has content hash but no content")
	}

	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return err
	}

	var contentTree *graviton.Tree = nil

	if leafData.Leaf.Content != nil {
		contentTree, err = snapshot.GetTree("content")
		if err != nil {
			return err
		}

		err = contentTree.Put(leafData.Leaf.ContentHash, leafData.Leaf.Content)
		if err != nil {
			return err
		}

		leafData.Leaf.Content = nil
	}

	var rootLeaf *merkle_dag.DagLeaf

	if leafData.Leaf.Hash == root {
		rootLeaf = &leafData.Leaf
	} else {
		_rootLeaf, err := store.RetrieveLeaf(root, root, false)
		if err != nil {
			return err
		}

		rootLeaf = &_rootLeaf.Leaf
	}

	bucket := GetBucket(rootLeaf)

	fmt.Printf("Adding to bucket: %s\n", bucket)

	cborData, err := cbor.Marshal(leafData)
	if err != nil {
		return err
	}

	key := leafData.Leaf.Hash // merkle_dag.GetHash(leaf.Hash)

	log.Printf("Adding key to block database: %s\n", key)

	tree, err := snapshot.GetTree(bucket)
	if err != nil {
		return err
	}

	err = tree.Put([]byte(key), cborData)
	if err != nil {
		return err
	}

	trees := []*graviton.Tree{}

	trees = append(trees, tree)

	if rootLeaf.Hash == leafData.Leaf.Hash {
		indexTree, err := snapshot.GetTree("root_index")
		if err != nil {
			return err
		}

		indexTree.Put([]byte(root), []byte(bucket))

		trees = append(trees, indexTree)

		if strings.HasPrefix(leafData.PublicKey, "npub") {
			userTree, err := snapshot.GetTree(leafData.PublicKey)
			if err == nil {
				value, err := userTree.Get([]byte(bucket))

				if err == nil && value != nil {
					var cacheData *types.CacheData = &types.CacheData{}

					err = cbor.Unmarshal(value, cacheData)
					if err == nil {
						cacheData.Keys = append(cacheData.Keys, root)
					}

					serializedData, err := cbor.Marshal(cacheData)
					if err == nil {
						tree.Put([]byte(bucket), serializedData)
					}
				} else {
					cacheData := &types.CacheData{
						Keys: []string{},
					}

					serializedData, err := cbor.Marshal(cacheData)
					if err == nil {
						tree.Put([]byte(bucket), serializedData)
					}
				}

				trees = append(trees, userTree)
			}
		}

		if configKey, ok := store.CacheConfig[bucket]; ok {
			valueOfLeaf := reflect.ValueOf(rootLeaf)
			value := valueOfLeaf.FieldByName(configKey)

			if value.IsValid() && value.Kind() == reflect.String {
				cacheKey := value.String()

				cacheTree, err := snapshot.GetTree(fmt.Sprintf("cache:%s", bucket))
				if err == nil {
					cacheTree.Put([]byte(cacheKey), []byte(root))

					fmt.Println("CACHE UPDATED: [" + bucket + "]" + cacheKey + ": " + root)

					trees = append(trees, cacheTree)
				}
			}
		}
	}

	if contentTree != nil {
		trees = append(trees, contentTree)
	}

	_, err = graviton.Commit(trees...)
	if err != nil {
		return err
	}

	return nil
}

func (store *GravitonMemoryStore) RetrieveLeafContent(contentHash []byte) ([]byte, error) {
	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return nil, err
	}

	contentTree, err := snapshot.GetTree("content")
	if err != nil {
		return nil, err
	}

	bytes, err := contentTree.Get(contentHash)
	if err != nil {
		return nil, err
	}

	if len(bytes) > 0 {
		return bytes, nil
	} else {
		return nil, fmt.Errorf("content not found")
	}
}

func (store *GravitonMemoryStore) retrieveBucket(root string) (string, error) {
	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return "", err
	}

	tree, err := snapshot.GetTree("root_index")
	if err != nil {
		return "", err
	}

	bytes, err := tree.Get([]byte(root))
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

func (store *GravitonMemoryStore) RetrieveLeaf(root string, hash string, includeContent bool) (*types.DagLeafData, error) {
	key := []byte(hash) // merkle_dag.GetHash(hash)

	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return nil, err
	}

	bucket, err := store.retrieveBucket(root)
	if err != nil {
		return nil, err
	}

	tree, err := snapshot.GetTree(bucket)
	if err != nil {
		return nil, err
	}

	log.Printf("Searching for leaf with key: %s\nFrom bucket: %s", key, bucket)
	bytes, err := tree.Get(key)
	if err != nil {
		return nil, err
	}

	var data *types.DagLeafData = &types.DagLeafData{}

	err = cbor.Unmarshal(bytes, data)
	if err != nil {
		return nil, err
	}

	if includeContent && data.Leaf.ContentHash != nil {
		fmt.Println("Fetching  leaf content")

		content, err := store.RetrieveLeafContent(data.Leaf.ContentHash)
		if err != nil {
			return nil, err
		}

		data.Leaf.Content = content
	}

	fmt.Println("Leaf found")

	return data, nil
}

// Retrieve and build an entire scionic merkletree from the root hash
func (store *GravitonMemoryStore) BuildDagFromStore(root string, includeContent bool) (*types.DagData, error) {
	return stores.BuildDagFromStore(store, root, includeContent)
}

// Store an entire scionic merkltree (not implemented currently as not required, leaves are stored as received)
func (store *GravitonMemoryStore) StoreDag(dag *types.DagData) error {
	return stores.StoreDag(store, dag)
}

func (store *GravitonMemoryStore) QueryEvents(filter nostr.Filter) ([]*nostr.Event, error) {
	log.Println("Processing filter:", filter)

	var events []*nostr.Event

	ss, _ := store.Database.LoadSnapshot(0)

	for _, kind := range filter.Kinds {
		tree, _ := ss.GetTree(fmt.Sprintf("kind:%d", kind))

		c := tree.Cursor()

		for _, v, err := c.First(); err == nil; _, v, err = c.Next() {
			var event nostr.Event
			if err := jsoniter.Unmarshal(v, &event); err != nil {
				continue
			}

			if filter.Matches(&event) {
				events = append(events, &event)
			}
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt > events[j].CreatedAt
	})

	if filter.Limit > 0 && len(events) > filter.Limit {
		events = events[:filter.Limit]
	}
	log.Println("Found", len(events), "matching events")

	return events, nil
}

func (store *GravitonMemoryStore) StoreEvent(event *nostr.Event) error {
	eventData, err := jsoniter.Marshal(event)
	if err != nil {
		return err
	}

	ss, _ := store.Database.LoadSnapshot(0)
	tree, _ := ss.GetTree(fmt.Sprintf("kind:%d", event.Kind))

	tree.Put([]byte(event.ID), eventData)

	graviton.Commit(tree)

	return nil
}

func (store *GravitonMemoryStore) DeleteEvent(eventID string) error {
	ss, _ := store.Database.LoadSnapshot(0)
	tree, _ := ss.GetTree("events")

	err := tree.Delete([]byte(eventID))
	if err != nil {
		return err
	} else {
		log.Println("Deleted event", eventID)
	}

	graviton.Commit(tree)

	return nil
}

func (store *GravitonMemoryStore) StoreBlob(data []byte, hash []byte, publicKey string) error {
	snapshot, _ := store.Database.LoadSnapshot(0)
	contentTree, _ := snapshot.GetTree("content")

	contentTree.Put(hash[:], data)

	graviton.Commit(contentTree)

	return nil
}

func (store *GravitonMemoryStore) GetBlob(hash string) ([]byte, error) {
	snapshot, _ := store.Database.LoadSnapshot(0)
	contentTree, _ := snapshot.GetTree("content")

	hashBytes, err := hex.DecodeString(hash)
	if err != nil {
		return nil, err
	}

	content, err := contentTree.Get(hashBytes)
	if err != nil {
		return nil, err
	}

	return content, nil
}

func (store *GravitonMemoryStore) DeleteBlob(hash string) error {
	snapshot, _ := store.Database.LoadSnapshot(0)
	contentTree, _ := snapshot.GetTree("content")

	hashBytes, err := hex.DecodeString(hash)
	if err != nil {
		return err
	}

	contentTree.Delete(hashBytes)

	graviton.Commit(contentTree)

	return nil
}

func (store *GravitonMemoryStore) SaveSubscriber(subscriber *types.Subscriber) error {
	// Load the snapshot and get the "subscribers" tree
	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %v", err)
	}

	subscriberTree, err := snapshot.GetTree("subscribers")
	if err != nil {
		return fmt.Errorf("failed to get subscribers tree: %v", err)
	}

	// Marshal the subscriber into JSON
	subscriberData, err := json.Marshal(subscriber)
	if err != nil {
		return fmt.Errorf("failed to marshal subscriber: %v", err)
	}

	// Use the npub as the key for storing the subscriber
	key := subscriber.Npub

	// Store the subscriber data in the tree
	if err := subscriberTree.Put([]byte(key), subscriberData); err != nil {
		return fmt.Errorf("failed to put subscriber in Graviton store: %v", err)
	}

	// Commit the tree to persist the changes
	if _, err := graviton.Commit(subscriberTree); err != nil {
		return fmt.Errorf("failed to commit subscribers tree: %v", err)
	}

	return nil
}

func (store *GravitonMemoryStore) GetSubscriberByAddress(address string) (*types.Subscriber, error) {
	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return nil, fmt.Errorf("failed to load snapshot: %v", err)
	}

	subscriberTree, err := snapshot.GetTree("subscribers")
	if err != nil {
		return nil, fmt.Errorf("failed to get subscribers tree: %v", err)
	}

	// Iterate over subscribers to find the one associated with the address
	cursor := subscriberTree.Cursor()
	for _, v, err := cursor.First(); err == nil; _, v, err = cursor.Next() {
		var subscriber types.Subscriber
		if err := json.Unmarshal(v, &subscriber); err != nil {
			return nil, fmt.Errorf("failed to unmarshal subscriber data: %v", err)
		}

		// Assuming the subscriber has a list of addresses

		if subscriber.Address == address {
			return &subscriber, nil
		}
	}

	return nil, fmt.Errorf("subscriber not found for address: %s", address)
}

func (store *GravitonMemoryStore) GetSubscriber(npub string) (*types.Subscriber, error) {
	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return nil, fmt.Errorf("failed to load snapshot: %v", err)
	}

	subscriberTree, err := snapshot.GetTree("subscribers")
	if err != nil {
		return nil, fmt.Errorf("failed to get subscribers tree: %v", err)
	}

	// Iterate over subscribers to find the one with the matching npub
	cursor := subscriberTree.Cursor()
	for _, v, err := cursor.First(); err == nil; _, v, err = cursor.Next() {
		var subscriber types.Subscriber
		if err := json.Unmarshal(v, &subscriber); err != nil {
			return nil, fmt.Errorf("failed to unmarshal subscriber data: %v", err)
		}

		// Check if the current subscriber's npub matches the provided npub
		if subscriber.Npub == npub {
			return &subscriber, nil
		}
	}

	// If no subscriber was found with the matching npub, return an error
	return nil, fmt.Errorf("subscriber not found for npub: %s", npub)
}

// AllocateBitcoinAddress allocates an available Bitcoin address to a subscriber.
func (store *GravitonMemoryStore) AllocateBitcoinAddress(npub string) (*types.Address, error) {
	// Load snapshot from the database
	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return nil, fmt.Errorf("failed to load snapshot: %v", err)
	}

	// Access the relay addresses tree
	addressTree, err := snapshot.GetTree("relay_addresses")
	if err != nil {
		return nil, fmt.Errorf("failed to get address tree: %v", err)
	}

	// Iterate through the addresses to find an available one
	cursor := addressTree.Cursor()
	for _, v, err := cursor.First(); err == nil; _, v, err = cursor.Next() {
		var addr types.Address
		if err := json.Unmarshal(v, &addr); err != nil {
			log.Printf("Error unmarshaling address: %v. Skipping this address.", err)
			continue
		}
		if addr.Status == AddressStatusAvailable {
			// Allocate the address to the subscriber
			now := time.Now()
			addr.Status = AddressStatusAllocated
			addr.AllocatedAt = &now
			addr.Npub = npub

			// Marshal the updated address and store it back in the database
			value, err := json.Marshal(addr)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal address: %v", err)
			}
			if err := addressTree.Put([]byte(addr.Index), value); err != nil {
				return nil, fmt.Errorf("failed to put address in tree: %v", err)
			}

			// Commit the changes to the database
			if _, err := graviton.Commit(addressTree); err != nil {
				return nil, fmt.Errorf("failed to commit address tree: %v", err)
			}

			return &addr, nil
		}
	}

	return nil, fmt.Errorf("no available addresses")
}

func (store *GravitonMemoryStore) SaveAddress(addr *types.Address) error {
	// Load the snapshot and get the "relay_addresses" tree
	snapshot, err := store.Database.LoadSnapshot(0)
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %v", err)
	}

	addressTree, err := snapshot.GetTree("relay_addresses")
	if err != nil {
		return fmt.Errorf("failed to get address tree: %v", err)
	}

	// Marshal the address into JSON
	addressData, err := json.Marshal(addr)
	if err != nil {
		return fmt.Errorf("failed to marshal address: %v", err)
	}

	// Use the index as the key for storing the address
	key := addr.Index

	// Store the address data in the tree
	if err := addressTree.Put([]byte(key), addressData); err != nil {
		return fmt.Errorf("failed to put address in Graviton store: %v", err)
	}

	// Commit the tree to persist the changes
	if _, err := graviton.Commit(addressTree); err != nil {
		return fmt.Errorf("failed to commit address tree: %v", err)
	}

	return nil
}

func GetBucket(leaf *merkle_dag.DagLeaf) string {
	hkind, ok := leaf.AdditionalData["hkind"]
	if ok {
		if hkind != "1" {
			return fmt.Sprintf("hkind:%s", hkind)
		}
	}

	split := strings.Split(leaf.ItemName, ".")

	if len(split) > 1 {
		return split[1]
	} else {
		if leaf.Type == merkle_dag.DirectoryLeafType {
			return "directory"
		} else {
			return "file"
		}
	}
}
