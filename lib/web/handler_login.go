package web

import (
	"log"
	"time"

	types "github.com/HORNET-Storage/hornet-storage/lib"
	"github.com/HORNET-Storage/hornet-storage/lib/stores"
	"github.com/gofiber/fiber/v2"
	"github.com/nbd-wtf/go-nostr"
)

// Refactored loginUser function
func loginUser(c *fiber.Ctx, store stores.Store) error {
	log.Println("Login request received")
	var loginPayload types.LoginPayload

	if err := c.BodyParser(&loginPayload); err != nil {
		log.Printf("Failed to parse JSON: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	// Find the user by npub
	user, err := store.GetStatsStore().FindUserByNpub(loginPayload.Npub)
	if err != nil {
		log.Printf("User not found: %v", err)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid npub or password",
		})
	}

	// Compare passwords
	if err := store.GetStatsStore().ComparePasswords(user.Password, loginPayload.Password); err != nil {
		log.Printf("Invalid password for user %s: %v", user.Npub, err)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Invalid npub or password",
		})
	}

	// Generate the challenge and hash
	challenge, hash, err := generateChallenge()
	if err != nil {
		log.Printf("Error generating challenge: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal server error",
		})
	}

	// Create the Nostr event
	event := &nostr.Event{
		PubKey:    user.Npub,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      1,
		Tags:      nostr.Tags{},
		Content:   challenge,
	}

	// Save the user challenge
	userChallenge := types.UserChallenge{
		UserID:    user.ID,
		Npub:      user.Npub,
		Challenge: challenge,
		Hash:      hash,
	}

	if err := store.GetStatsStore().SaveUserChallenge(&userChallenge); err != nil {
		log.Printf("Failed to save challenge: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Internal server error",
		})
	}

	log.Printf("Login challenge created for user %s", user.Npub)

	// Return the event as JSON
	return c.JSON(fiber.Map{
		"event": event,
	})
}
