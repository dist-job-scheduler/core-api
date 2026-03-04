// Package stripe wraps the Stripe SDK with the minimal surface area needed by
// the billing usecase. Keeping it thin makes the usecase easy to test with a
// fake/mock implementation.
package stripe

import (
	"fmt"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/webhook"
)

// Client is a thin wrapper around the Stripe SDK.
type Client struct {
	webhookSecret string
}

// New returns a Client configured with the given secret key and webhook secret.
func New(secretKey, webhookSecret string) *Client {
	stripe.Key = secretKey
	return &Client{webhookSecret: webhookSecret}
}

// CreateCustomer creates a new Stripe customer and returns its ID.
func (c *Client) CreateCustomer(email string) (string, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
	}
	cust, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe create customer: %w", err)
	}
	return cust.ID, nil
}

// CreateCheckoutSession creates a Stripe Checkout Session for a one-time payment.
// metadata is attached to the session so the webhook can identify the user and pack.
func (c *Client) CreateCheckoutSession(customerID, priceID, successURL, cancelURL string, metadata map[string]string) (string, error) {
	params := &stripe.CheckoutSessionParams{
		Customer: stripe.String(customerID),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
	}
	for k, v := range metadata {
		params.AddMetadata(k, v)
	}

	sess, err := session.New(params)
	if err != nil {
		return "", fmt.Errorf("stripe create checkout session: %w", err)
	}
	return sess.URL, nil
}

// ConstructEvent parses and verifies a Stripe webhook payload.
func (c *Client) ConstructEvent(payload []byte, sigHeader string) (stripe.Event, error) {
	event, err := webhook.ConstructEvent(payload, sigHeader, c.webhookSecret)
	if err != nil {
		return stripe.Event{}, fmt.Errorf("stripe construct event: %w", err)
	}
	return event, nil
}
