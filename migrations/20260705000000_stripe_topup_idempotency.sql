-- +goose Up

-- Make Stripe top-ups idempotent. Stripe delivers webhooks at-least-once and
-- retries checkout.session.completed whenever it doesn't see a timely 2xx, so a
-- redelivery must not credit the same payment twice. A partial unique index on
-- the payment intent (top-up rows only) turns a duplicate delivery into a
-- no-op via ON CONFLICT in the TopUp repository method. NULL payment intents
-- (rare events with no PaymentIntent) are excluded and remain unconstrained.
--
-- NOTE: if this index fails to build with a unique_violation, pre-existing
-- duplicate top-ups already exist in the ledger from before this fix. Do NOT
-- remove the check — reconcile the duplicates first:
--   SELECT stripe_payment_intent_id, count(*) FROM credit_transactions
--   WHERE type = 'stripe_topup' AND stripe_payment_intent_id IS NOT NULL
--   GROUP BY 1 HAVING count(*) > 1;
CREATE UNIQUE INDEX IF NOT EXISTS uq_credit_tx_stripe_topup_payment_intent
    ON credit_transactions (stripe_payment_intent_id)
    WHERE type = 'stripe_topup' AND stripe_payment_intent_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS uq_credit_tx_stripe_topup_payment_intent;
