package kind1

import (
	"log"

	"github.com/HORNET-Storage/hornet-storage/lib/stores"
	jsoniter "github.com/json-iterator/go"

	"github.com/nbd-wtf/go-nostr"

	lib_nostr "github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr"
)

func BuildKind1Handler(store stores.Store) func(read lib_nostr.KindReader, write lib_nostr.KindWriter) {
	handler := func(read lib_nostr.KindReader, write lib_nostr.KindWriter) {
		// Use Jsoniter for JSON operations
		var json = jsoniter.ConfigCompatibleWithStandardLibrary

		log.Println("Working with default kind one handler.")

		// Read data from the stream
		data, err := read()
		if err != nil {
			write("NOTICE", "Error reading from stream.")
			return
		}

		// Unmarshal the received data into a Nostr event
		var env nostr.EventEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			write("NOTICE", "Error unmarshaling event.")
			return
		}

		event := env.Event

		// Check if the event is of kind 1
		if event.Kind != 1 {
			log.Printf("Received non-kind-1 event on kind-1 handler, ignoring.")
			return
		}

		log.Printf("Processing kind 1 event: %s", event.Content)

		// Perform time check
		isValid, errMsg := lib_nostr.TimeCheck(event.CreatedAt.Time().Unix())
		if !isValid {
			// If the timestamp is invalid, respond with an error message and return early
			log.Println(errMsg)
			write("OK", event.ID, false, errMsg)
			return
		}

		// Store the event
		if err := store.StoreEvent(&event); err != nil {
			// Example: Sending an "OK" message with an error indication
			write("OK", event.ID, false, "error storing event")
		} else {
			// Example: Successfully stored event, sending a success "OK" message
			write("OK", event.ID, true, "")
		}
	}

	return handler
}
