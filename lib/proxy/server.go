package proxy

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	jsoniter "github.com/json-iterator/go"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/viper"

	"github.com/HORNET-Storage/hornet-storage/lib/blossom"
	lib_nostr "github.com/HORNET-Storage/hornet-storage/lib/handlers/nostr"
	"github.com/HORNET-Storage/hornet-storage/lib/stores"
)

func StartServer(store stores.Store) error {
	// Generate the global challenge
	_, err := generateGlobalChallenge()
	if err != nil {
		log.Fatalf("Failed to generate global challenge: %v", err)
	}

	app := fiber.New()
	app.Use(handleRelayInfoRequests)
	app.Get("/", websocket.New(handleWebSocketConnections))

	if viper.GetBool("blossom") {
		server := blossom.NewServer(store)
		server.SetupRoutes(app)
	}

	port := viper.GetString("port")
	p, err := strconv.Atoi(port)
	if err != nil {
		log.Fatalf("Error parsing port %s: %v", port, err)
	}

	for {
		port := fmt.Sprintf(":%d", p+1)
		err := app.Listen(port)
		if err != nil {
			log.Printf("Error starting web-server: %v\n", err)
			if strings.Contains(err.Error(), "address already in use") {
				p += 1
			} else {
				break
			}
		} else {
			break
		}
	}
	return err
}

// Middleware function to respond with relay information on GET requests
func handleRelayInfoRequests(c *fiber.Ctx) error {
	if c.Method() == "GET" && c.Get("Accept") == "application/nostr+json" {
		relayInfo := getRelayInfo()
		c.Set("Access-Control-Allow-Origin", "*")
		return c.JSON(relayInfo)
	}
	return c.Next()
}

func getRelayInfo() nip11RelayInfo {
	return nip11RelayInfo{
		Name:          viper.GetString("RelayName"),
		Description:   viper.GetString("RelayDescription"),
		Pubkey:        viper.GetString("RelayPubkey"),
		Contact:       viper.GetString("RelayContact"),
		SupportedNIPs: []int{1, 11, 2, 9, 18, 23, 24, 25, 51, 56, 57, 42},
		Software:      viper.GetString("RelaySoftware"),
		Version:       viper.GetString("RelayVersion"),
	}
}

// Handles WebSocket connections and their lifecycles
func handleWebSocketConnections(c *websocket.Conn) {
	defer removeListener(c)

	challenge := getGlobalChallenge()
	log.Printf("Using global challenge for connection: %s", challenge)

	for {
		if err := processWebSocketMessage(c, challenge); err != nil {
			log.Printf("Error processing WebSocket message: %v\n", err)
			break
		}
	}
}

func processWebSocketMessage(c *websocket.Conn, challenge string) error {
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	_, message, err := c.ReadMessage()
	if err != nil {
		return fmt.Errorf("read error: %w", err)
	}

	rawMessage := nostr.ParseMessage(message)

	switch env := rawMessage.(type) {
	case *nostr.EventEnvelope:
		log.Println("Received EVENT message:", env.Kind)

		settings, err := lib_nostr.LoadRelaySettings()
		if err != nil {
			log.Fatalf("Failed to load relay settings: %v", err)
			return err
		}

		if settings.Mode == "unlimited" {
			log.Println("Unlimited Mode processing.")
			handler := lib_nostr.GetHandler("universal")

			if handler != nil {
				notifyListeners(&env.Event)

				read := func() ([]byte, error) {
					bytes, err := json.Marshal(env)
					if err != nil {
						return nil, err
					}

					return bytes, nil
				}

				write := func(messageType string, params ...interface{}) {
					response := lib_nostr.BuildResponse(messageType, params)

					if len(response) > 0 {
						handleIncomingMessage(c, response)
					}
				}

				if verifyNote(&env.Event) {
					handler(read, write)
				} else {
					write("OK", env.ID, false, "Invalid note")
				}
			}
		} else if settings.Mode == "smart" {
			handler := lib_nostr.GetHandler(fmt.Sprintf("kind/%d", env.Kind))

			if handler != nil {
				notifyListeners(&env.Event)

				read := func() ([]byte, error) {
					bytes, err := json.Marshal(env)
					if err != nil {
						return nil, err
					}

					return bytes, nil
				}

				write := func(messageType string, params ...interface{}) {
					response := lib_nostr.BuildResponse(messageType, params)

					if len(response) > 0 {
						handleIncomingMessage(c, response)
					}
				}

				if verifyNote(&env.Event) {
					handler(read, write)
				} else {
					write("OK", env.ID, false, "Invalid note")
				}
			}
		}
	case *nostr.ReqEnvelope:
		handler := lib_nostr.GetHandler("filter")

		if handler != nil {
			_, cancelFunc := context.WithCancel(context.Background())

			setListener(env.SubscriptionID, c, env.Filters, cancelFunc)

			response := lib_nostr.BuildResponse("AUTH", challenge)

			if len(response) > 0 {
				handleIncomingMessage(c, response)
			}

			read := func() ([]byte, error) {
				bytes, err := json.Marshal(env)
				if err != nil {
					return nil, err
				}

				return bytes, nil
			}

			write := func(messageType string, params ...interface{}) {
				response := lib_nostr.BuildResponse(messageType, params)

				if len(response) > 0 {
					handleIncomingMessage(c, response)
				}
			}

			handler(read, write)
		}
	case *nostr.CloseEnvelope:
		var closeEvent []string
		err := json.Unmarshal([]byte(env.String()), &closeEvent)
		if err != nil {
			fmt.Println("Error:", err)
			// Send a NOTICE message in case of unmarshalling error
			errMsg := "Error unmarshalling CLOSE request: " + err.Error()
			if writeErr := sendWebSocketMessage(c, nostr.NoticeEnvelope(errMsg)); writeErr != nil {
				log.Println("Error sending NOTICE message:", writeErr)
			}
			return err
		}
		subscriptionID := closeEvent[1]

		// Assume removeListenerId will be called
		responseMsg := nostr.ClosedEnvelope{SubscriptionID: subscriptionID, Reason: "Subscription closed successfully."}
		// Attempt to remove the listener for the given subscription ID
		removeListenerId(c, subscriptionID)

		// Send the prepared CLOSED or error message
		if err := sendWebSocketMessage(c, responseMsg); err != nil {
			log.Printf("Error sending 'CLOSED' envelope over WebSocket: %v", err)
		}

	case *nostr.CountEnvelope:
		handler := lib_nostr.GetHandler("count")

		if handler != nil {
			_, cancelFunc := context.WithCancel(context.Background())

			setListener(env.SubscriptionID, c, env.Filters, cancelFunc)

			response := lib_nostr.BuildResponse("AUTH", challenge)

			if len(response) > 0 {
				handleIncomingMessage(c, response)
			}

			read := func() ([]byte, error) {
				bytes, err := json.Marshal(env)
				if err != nil {
					return nil, err
				}

				return bytes, nil
			}

			write := func(messageType string, params ...interface{}) {
				response := lib_nostr.BuildResponse(messageType, params)

				if len(response) > 0 {
					handleIncomingMessage(c, response)
				}
			}

			handler(read, write)
		}
	case *nostr.AuthEnvelope:
		return handleAuthMessage(c, env, challenge)
	default:
		log.Println("Unknown message type:")
	}

	return nil
}

func handleAuthMessage(c *websocket.Conn, env *nostr.AuthEnvelope, challenge string) error {
	write := func(messageType string, params ...interface{}) {
		response := lib_nostr.BuildResponse(messageType, params)
		if len(response) > 0 {
			handleIncomingMessage(c, response)
		}
	}

	if env.Event.Kind != 22242 {
		write("OK", env.Event.ID, false, "Error auth event kind must be 22242")
		return nil
	}

	isValid, errMsg := lib_nostr.AuthTimeCheck(env.Event.CreatedAt.Time().Unix())
	if !isValid {
		write("OK", env.Event.ID, false, errMsg)
		return nil
	}

	result, err := env.Event.CheckSignature()
	if err != nil || !result {
		write("OK", env.Event.ID, false, "Error checking event signature")
		return nil
	}

	var hasRelayTag, hasChallengeTag bool
	for _, tag := range env.Event.Tags {
		if len(tag) >= 2 {
			if tag[0] == "relay" {
				hasRelayTag = true
			} else if tag[0] == "challenge" {
				hasChallengeTag = true
				if tag[1] != challenge {
					write("OK", env.Event.ID, false, "Error checking session challenge")
					return nil
				}
			}
		}
	}

	if !hasRelayTag || !hasChallengeTag {
		write("CLOSE", env.Event.ID, false, "Error event does not have required tags")
		return nil
	}

	err = AuthenticateConnection(c)
	if err != nil {
		write("OK", env.Event.ID, false, "Error authorizing connection")
		return nil
	}

	log.Println("Connection successfully authenticated.")

	write("OK", env.Event.ID, true, "")
	return nil
}
