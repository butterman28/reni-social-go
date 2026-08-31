package billing

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"log"
	"reni-social/pkg/config"

	"github.com/gofiber/fiber/v2"
)

// PaymentWebhook handles callbacks from Paystack (or Stripe)
func PaymentWebhook(c *fiber.Ctx) error {
	// 1. VERIFY SIGNATURE (Crucial for security)
	// Paystack sends the hash in the "x-paystack-signature" header
	signature := c.Get("x-paystack-signature")
	if signature == "" {
		log.Println(" [Webhook] Rejected: Missing signature header")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "missing signature"})
	}

	// Compute the expected HMAC SHA512 hash of the raw request body
	hash := hmac.New(sha512.New, []byte(config.C.PaystackSecretKey))
	hash.Write(c.Body())
	expectedSig := "sha512=" + hex.EncodeToString(hash.Sum(nil))

	// Constant-time comparison to prevent timing attacks
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		log.Println(" [Webhook] Rejected: Invalid signature")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid signature"})
	}

	// 2. PARSE PAYLOAD
	var payload struct {
		Event string `json:"event"`
		Data  struct {
			Reference string `json:"reference"` // Unique transaction ID from gateway
			Amount    int    `json:"amount"`    // Amount in Kobo/Cents (e.g., 5000 = 50.00)
			Metadata  struct {
				UserID string `json:"user_id"` // Passed from your frontend during checkout
			} `json:"metadata"`
		} `json:"data"`
	}

	if err := c.BodyParser(&payload); err != nil {
		log.Printf(" [Webhook] Rejected: Invalid payload - %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
	}

	// 3. PROCESS ONLY SUCCESSFUL CHARGES
	if payload.Event == "charge.success" {
		if payload.Data.Metadata.UserID == "" {
			log.Println(" [Webhook] Rejected: Missing user_id in metadata")
			return c.Status(fiber.StatusBadRequest).SendStatus(fiber.StatusOK) // Return 200 so gateway stops retrying
		}

		// Convert gateway amount to your internal credits.
		// Example: Paystack amount is in Kobo (5000 kobo = 50 Naira). 
		// If 1 Credit = 1 Naira, then credits = Amount / 100
		credits := payload.Data.Amount / 100 

		// 4. ADD CREDITS (Idempotent due to UNIQUE 'reference' in DB)
		err := AddCredits(c.Context(), payload.Data.Metadata.UserID, credits, "top_up", payload.Data.Reference)
		if err != nil {
			// If it's a unique constraint violation, it means we already processed this webhook.
			// We still return 200 OK so the gateway knows we got it.
			log.Printf(" [Webhook] Note: %v", err)
		} else {
			log.Printf(" [Webhook] Success: Added %d credits to user %s (Ref: %s)", credits, payload.Data.Metadata.UserID, payload.Data.Reference)
		}
	}

	// 5. ALWAYS RETURN 200 OK
	// If you return 4xx or 5xx, Paystack/Stripe will assume delivery failed 
	// and will aggressively retry, causing duplicate processing attempts.
	return c.SendStatus(fiber.StatusOK)
}