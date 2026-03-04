package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	stripeclient "github.com/ErlanBelekov/dist-job-scheduler/internal/stripe"
	stripe "github.com/stripe/stripe-go/v82"
)

const stripeMinimumCents = 50 // Stripe enforces a $0.50 floor on USD charges

// BillingConfig holds the exchange rate and Stripe Checkout redirect URLs.
type BillingConfig struct {
	// CreditsPerDollar is the exchange rate (e.g. 1000 = 1000 credits per $1).
	CreditsPerDollar int64
	SuccessURL       string
	CancelURL        string
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

// CreditsPerDollar returns the configured exchange rate so the frontend can
// build a cost calculator without a separate config endpoint.
func (u *BillingUsecase) CreditsPerDollar() int64 {
	return u.config.CreditsPerDollar
}

// CreateCheckoutSession creates a Stripe Checkout Session for a custom credit top-up.
// credits is the number of credits the user wants to purchase; the price is derived
// from the CreditsPerDollar exchange rate.
func (u *BillingUsecase) CreateCheckoutSession(ctx context.Context, userID string, credits int64) (string, error) {
	if credits <= 0 {
		return "", fmt.Errorf("credits must be positive")
	}

	amountCents := (credits * 100) / u.config.CreditsPerDollar
	if amountCents < stripeMinimumCents {
		minCredits := stripeMinimumCents * u.config.CreditsPerDollar / 100
		return "", fmt.Errorf("minimum purchase is %d credits ($%.2f)", minCredits, float64(stripeMinimumCents)/100)
	}

	// Look up or create a Stripe customer.
	customerID, err := u.stripeCustomers.FindByUserID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("find stripe customer: %w", err)
	}
	if customerID == "" {
		var user *domain.User
		user, err = u.users.FindByID(ctx, userID)
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
		if err = u.stripeCustomers.Save(ctx, userID, customerID); err != nil {
			return "", fmt.Errorf("save stripe customer: %w", err)
		}
	}

	description := fmt.Sprintf("%s credits", formatCredits(credits))

	checkoutURL, err := u.stripe.CreateCheckoutSession(
		customerID,
		amountCents,
		description,
		u.config.SuccessURL,
		u.config.CancelURL,
		map[string]string{
			"user_id": userID,
			"credits": strconv.FormatInt(credits, 10),
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
	if err = json.Unmarshal(event.Data.Raw, &sess); err != nil {
		return fmt.Errorf("unmarshal checkout session: %w", err)
	}

	userID := sess.Metadata["user_id"]
	creditsStr := sess.Metadata["credits"]
	if userID == "" || creditsStr == "" {
		u.logger.WarnContext(ctx, "checkout.session.completed missing metadata",
			"session_id", sess.ID)
		return nil
	}

	credits, err := strconv.ParseInt(creditsStr, 10, 64)
	if err != nil {
		u.logger.WarnContext(ctx, "checkout.session.completed invalid credits metadata",
			"session_id", sess.ID,
			"credits_raw", creditsStr,
		)
		return nil
	}

	paymentIntentID := ""
	if sess.PaymentIntent != nil {
		paymentIntentID = sess.PaymentIntent.ID
	}

	if err := u.credits.TopUp(ctx, userID, credits, paymentIntentID); err != nil {
		return fmt.Errorf("top up credits for user %s: %w", userID, err)
	}

	// After a purchase, upgrade the user to the paid plan.
	if err := u.credits.UpdatePlan(ctx, userID, domain.PlanPaid); err != nil {
		u.logger.WarnContext(ctx, "failed to upgrade plan after topup", "user_id", userID, "error", err)
		// Non-fatal: credits were already added.
	}

	u.logger.InfoContext(ctx, "credits topped up",
		"user_id", userID,
		"credits", credits,
		"payment_intent_id", paymentIntentID,
	)
	return nil
}

// formatCredits formats a credit amount with thousands separators for display.
func formatCredits(n int64) string {
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	result := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
