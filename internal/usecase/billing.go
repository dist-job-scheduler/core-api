package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	stripeclient "github.com/ErlanBelekov/dist-job-scheduler/internal/stripe"
	stripe "github.com/stripe/stripe-go/v82"
)

// CreditPack describes a purchasable credit bundle.
type CreditPack struct {
	Name    string
	Credits int64
	PriceID string
}

// BillingConfig holds Stripe price IDs and pack definitions.
type BillingConfig struct {
	Packs      []CreditPack
	SuccessURL string
	CancelURL  string
}

// BillingUsecase handles credit balance queries, Stripe checkout, and webhook processing.
type BillingUsecase struct {
	credits         repository.CreditRepository
	stripeCustomers repository.StripeCustomerRepository
	users           repository.UserRepository
	stripe          *stripeclient.Client
	config          BillingConfig
	logger          *slog.Logger
}

func NewBillingUsecase(
	credits repository.CreditRepository,
	stripeCustomers repository.StripeCustomerRepository,
	users repository.UserRepository,
	stripeClient *stripeclient.Client,
	config BillingConfig,
	logger *slog.Logger,
) *BillingUsecase {
	return &BillingUsecase{
		credits:         credits,
		stripeCustomers: stripeCustomers,
		users:           users,
		stripe:          stripeClient,
		config:          config,
		logger:          logger.With("component", "billing_usecase"),
	}
}

func (u *BillingUsecase) GetBalance(ctx context.Context, userID string) (*domain.CreditBalance, error) {
	b, err := u.credits.GetBalance(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	return b, nil
}

// CreateCheckoutSession looks up (or creates) the user's Stripe customer, then
// creates a one-time Checkout Session for the requested credit pack.
func (u *BillingUsecase) CreateCheckoutSession(ctx context.Context, userID, packName string) (string, error) {
	// Resolve the requested pack.
	var pack *CreditPack
	for i := range u.config.Packs {
		if u.config.Packs[i].Name == packName {
			pack = &u.config.Packs[i]
			break
		}
	}
	if pack == nil {
		return "", fmt.Errorf("unknown pack: %s", packName)
	}

	// Look up or create a Stripe customer.
	customerID, err := u.stripeCustomers.FindByUserID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("find stripe customer: %w", err)
	}
	if customerID == "" {
		user, err := u.users.FindByID(ctx, userID)
		if err != nil {
			return "", fmt.Errorf("find user: %w", err)
		}
		email := ""
		if user.Email != nil {
			email = *user.Email
		}
		customerID, err = u.stripe.CreateCustomer(email)
		if err != nil {
			return "", fmt.Errorf("create stripe customer: %w", err)
		}
		if err := u.stripeCustomers.Save(ctx, userID, customerID); err != nil {
			return "", fmt.Errorf("save stripe customer: %w", err)
		}
	}

	// Create the Checkout Session with metadata so the webhook can attribute it.
	checkoutURL, err := u.stripe.CreateCheckoutSession(
		customerID,
		pack.PriceID,
		u.config.SuccessURL,
		u.config.CancelURL,
		map[string]string{
			"user_id":   userID,
			"pack_name": packName,
		},
	)
	if err != nil {
		return "", fmt.Errorf("create checkout session: %w", err)
	}
	return checkoutURL, nil
}

// HandleWebhook verifies and processes a Stripe webhook event.
// Only checkout.session.completed is handled; all others are acknowledged silently.
func (u *BillingUsecase) HandleWebhook(ctx context.Context, payload []byte, sigHeader string) error {
	event, err := u.stripe.ConstructEvent(payload, sigHeader)
	if err != nil {
		return fmt.Errorf("construct event: %w", err)
	}

	if event.Type != "checkout.session.completed" {
		return nil
	}

	var sess stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		return fmt.Errorf("unmarshal checkout session: %w", err)
	}

	userID := sess.Metadata["user_id"]
	packName := sess.Metadata["pack_name"]
	if userID == "" || packName == "" {
		u.logger.WarnContext(ctx, "checkout.session.completed missing metadata",
			"session_id", sess.ID)
		return nil
	}

	var pack *CreditPack
	for i := range u.config.Packs {
		if u.config.Packs[i].Name == packName {
			pack = &u.config.Packs[i]
			break
		}
	}
	if pack == nil {
		u.logger.WarnContext(ctx, "checkout.session.completed unknown pack",
			"pack_name", packName,
			"session_id", sess.ID)
		return nil
	}

	paymentIntentID := ""
	if sess.PaymentIntent != nil {
		paymentIntentID = sess.PaymentIntent.ID
	}

	if err := u.credits.TopUp(ctx, userID, pack.Credits, paymentIntentID); err != nil {
		return fmt.Errorf("top up credits for user %s: %w", userID, err)
	}

	// After a paid purchase, upgrade the user to the paid plan.
	if err := u.credits.UpdatePlan(ctx, userID, domain.PlanPaid); err != nil {
		u.logger.WarnContext(ctx, "failed to upgrade plan after topup", "user_id", userID, "error", err)
		// Non-fatal: credits were already added.
	}

	u.logger.InfoContext(ctx, "credits topped up",
		"user_id", userID,
		"pack", packName,
		"credits", pack.Credits,
		"payment_intent_id", paymentIntentID,
	)
	return nil
}
