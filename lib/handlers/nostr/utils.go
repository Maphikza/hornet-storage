package nostr

import (
	"fmt"
	"log"

	"time"

	"github.com/fxamacker/cbor/v2"
	jsoniter "github.com/json-iterator/go"
	"github.com/libp2p/go-libp2p/core/network"
)

// Returns true if the event timestamp is valid, or false with an error message if it's too far off.
func TimeCheck(eventCreatedAt int64) (bool, string) {
	const timeCutoff = 5 * time.Minute // Define your own cutoff threshold
	eventTime := time.Unix(eventCreatedAt, 0)

	// Check if the event timestamp is too far in the past or future
	if time.Since(eventTime) > timeCutoff || time.Until(eventTime) > timeCutoff {
		errMsg := fmt.Sprintf("invalid: event creation date is too far off from the current time (%s)", eventTime)
		return false, errMsg
	}
	return true, ""
}

// responder sends a response string through the given network stream
func Responder(stream network.Stream, messageType string, params ...interface{}) {
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	var message []interface{}
	message = append(message, messageType)
	message = append(message, params...)
	jsonMessage, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling response message: %s\n", err)
		return
	}

	log.Println("Writing to stream:", string(jsonMessage))

	// Write the JSON message to the stream
	if _, err := stream.Write(jsonMessage); err != nil {
		log.Printf("Error writing to stream: %s\n", err)
	}
}

func BuildResponse(messageType string, params ...interface{}) []byte {
	var json = jsoniter.ConfigCompatibleWithStandardLibrary

	// Extract and flatten values from params before appending to the message
	extractedParams := extractInterfaceValues(params...)

	var message []interface{}
	message = append(message, messageType)
	// Append the extracted parameters individually to ensure a flat structure
	message = append(message, extractedParams...)

	log.Println("Checking how message looks.", message)

	jsonMessage, err := json.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling response message: %s\n", err)
		return nil
	}

	// Append a newline character to the JSON message to act as a delimiter
	jsonMessageWithDelimiter := append(jsonMessage, '\n')

	return jsonMessageWithDelimiter
}

func BuildCborResponse(messageType string, params ...interface{}) []byte {
	// Extract and flatten values from params before appending to the message
	extractedParams := extractInterfaceValues(params...)

	var message []interface{}
	message = append(message, messageType)
	// Append the extracted parameters individually to ensure a flat structure
	message = append(message, extractedParams...)

	log.Println("Checking how message looks.", message)

	cborMessage, err := cbor.Marshal(message)
	if err != nil {
		log.Printf("Error marshaling response message: %s\n", err)
		return nil
	}

	return cborMessage
}

func extractInterfaceValues(data ...interface{}) []interface{} {
	var extracted []interface{}
	for _, v := range data {
		switch element := v.(type) {
		case []interface{}:
			// Recursively flatten nested slices
			extracted = append(extracted, extractInterfaceValues(element...)...)
		default:
			extracted = append(extracted, element)
		}
	}
	return extracted
}

func CloseStream(stream network.Stream) {
	if err := stream.CloseWrite(); err != nil {
		log.Printf("Error closing stream: %s\n", err)
	}
}
