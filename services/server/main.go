package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/HORNET-Storage/hornet-storage/lib/handlers/count"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/filter"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind0"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind1"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind10000"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind1984"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind3"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind30000"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind30008"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind30009"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind30023"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind36810"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind5"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind6"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind7"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind8"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind9372"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind9373"
	"github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr/kind9735"
	universalhandler "github.com/HORNET-Storage/hornet-storage/lib/handlers/universal"
	"github.com/HORNET-Storage/hornet-storage/lib/proxy"
	"github.com/HORNET-Storage/hornet-storage/lib/signing"
	"github.com/HORNET-Storage/hornet-storage/lib/web"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/fsnotify/fsnotify"
	"github.com/ipfs/go-cid"
	"github.com/spf13/viper"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"

	//"github.com/libp2p/go-libp2p/p2p/security/noise"
	//libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"

	"github.com/HORNET-Storage/hornet-storage/lib/handlers"

	merkle_dag "github.com/HORNET-Storage/scionic-merkletree/dag"

	//stores_bbolt "github.com/HORNET-Storage/hornet-storage/lib/stores/bbolt"
	//stores_memory "github.com/HORNET-Storage/hornet-storage/lib/stores/memory"
	stores_graviton "github.com/HORNET-Storage/hornet-storage/lib/stores/graviton"
	//negentropy "github.com/illuzen/go-negentropy"
)

func init() {
	viper.SetDefault("key", "")
	viper.SetDefault("web", false)
	viper.SetDefault("proxy", true)
	viper.SetDefault("query_cache", map[string]string{
		"hkind:2": "ItemName",
	})
	viper.SetDefault("service_tag", "hornet-storage-service")

	viper.AddConfigPath(".")
	viper.SetConfigType("json")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			viper.SafeWriteConfig()
		}
	}

	viper.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("Config file changed:", e.Name)
	})

	viper.WatchConfig()
}

func main() {
	wg := new(sync.WaitGroup)
	ctx := context.Background()

	// Private key
	key := viper.GetString("key")

	host := web.GetHost(key)

	// Create and initialize database
	store := &stores_graviton.GravitonStore{}

	queryCache := viper.GetStringMapString("query_cache")
	store.InitStore(queryCache)


	// Stream Handlers
	handlers.AddDownloadHandler(host, store, func(rootLeaf *merkle_dag.DagLeaf, pubKey *string, signature *string) bool {
		return true
	})

	handlers.AddUploadHandler(host, store, func(rootLeaf *merkle_dag.DagLeaf, pubKey *string, signature *string) bool {
		decodedSignature, err := hex.DecodeString(*signature)
		if err != nil {
			fmt.Println("2")
			return false
		}

		parsedSignature, err := schnorr.ParseSignature(decodedSignature)
		if err != nil {
			fmt.Println("3")
			return false
		}

		cid, err := cid.Parse(rootLeaf.Hash)
		if err != nil {
			fmt.Println("4")
			return false
		}

		fmt.Println(*pubKey)

		publicKey, err := signing.DeserializePublicKey(*pubKey)
		if err != nil {
			fmt.Printf("err: %vzn", err)
			fmt.Println("5")
			return false
		}

		err = signing.VerifyCIDSignature(parsedSignature, cid, publicKey)
		return err == nil
	}, func(dag *merkle_dag.Dag, pubKey *string) {})

	// Register Our Nostr Stream Handlers
	nostr.RegisterHandler("universal", universalhandler.BuildUniversalHandler(store))
	nostr.RegisterHandler("kind/0", kind0.BuildKind0Handler(store))
	nostr.RegisterHandler("kind/1", kind1.BuildKind1Handler(store))
	nostr.RegisterHandler("kind/3", kind3.BuildKind3Handler(store))
	nostr.RegisterHandler("kind/5", kind5.BuildKind5Handler(store))
	nostr.RegisterHandler("kind/6", kind6.BuildKind6Handler(store))
	nostr.RegisterHandler("kind/7", kind7.BuildKind7Handler(store))
	nostr.RegisterHandler("kind/8", kind8.BuildKind8Handler(store))
	nostr.RegisterHandler("kind/1984", kind1984.BuildKind1984Handler(store))
	nostr.RegisterHandler("kind/9735", kind9735.BuildKind9735Handler(store))
	nostr.RegisterHandler("kind/9372", kind9372.BuildKind9372Handler(store))
	nostr.RegisterHandler("kind/9373", kind9373.BuildKind9373Handler(store))
	nostr.RegisterHandler("kind/30023", kind30023.BuildKind30023Handler(store))
	nostr.RegisterHandler("kind/10000", kind10000.BuildKind10000Handler(store))
	nostr.RegisterHandler("kind/30000", kind30000.BuildKind30000Handler(store))
	nostr.RegisterHandler("kind/30008", kind30008.BuildKind30008Handler(store))
	nostr.RegisterHandler("kind/30009", kind30009.BuildKind30009Handler(store))
	nostr.RegisterHandler("kind/36810", kind36810.BuildKind36810Handler(store))
	nostr.RegisterHandler("filter", filter.BuildFilterHandler(store))
	nostr.RegisterHandler("count", count.BuildCountsHandler(store))

	err := error(nil)
	// Register a libp2p handler for every stream handler
	for kind := range nostr.GetHandlers() {
		handler := nostr.GetHandler(kind)

		wrapper := func(stream network.Stream) {
			read := func() ([]byte, error) {
				decoder := json.NewDecoder(stream)

				var rawMessage json.RawMessage
				err := decoder.Decode(&rawMessage)
				if err != nil {
					return nil, err
				}

				return rawMessage, nil
			}

			write := func(messageType string, params ...interface{}) {
				response := nostr.BuildResponse(messageType, params)

				if len(response) > 0 {
					stream.Write(response)
				}

				if err == nil {
					fmt.Printf("Response written to stream: %s", string(response))
				}
			}

			handler(read, write)

			stream.Close()
		}

		host.SetStreamHandler(protocol.ID("/nostr/event/"+kind), wrapper)
	}

	// Web Panel
	if viper.GetBool("web") {
		wg.Add(1)

		fmt.Println("Starting with web server enabled")

		go func() {
			err := web.StartServer()

			if err != nil {
				fmt.Println("Fatal error occurred in web server")
			}

			wg.Done()
		}()
	}

	// Proxy web sockets
	if viper.GetBool("proxy") {
		wg.Add(1)

		fmt.Println("Starting with legacy nostr proxy web server enabled")

		go func() {
			err := proxy.StartServer()

			if err != nil {
				fmt.Println("Fatal error occurred in web server")
			}

			wg.Done()
		}()
	}


	if err := setupMDNS(host, viper.GetString("serviceTag"), ctx); err != nil {
		log.Fatal(err)
	}

	// Wait a bit for discovery to happen
	time.Sleep(20 * time.Second)

	// Print out the peers each host knows about
	fmt.Println("Host 1 peers:")
	for _, p := range host.Peerstore().Peers() {
		fmt.Println(p)
	}

	defer host.Close()

	wg.Wait()
}

