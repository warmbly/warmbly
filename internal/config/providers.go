package config

import "os"

// Provider selection helpers. These centralize the env-var switches and their
// defaults so the service mains (and the AWS-config gate below) agree on which
// backend is active without each re-deriving it.

// KMSProvider returns the selected KMS provider ("aws" | "local"). Default aws.
func KMSProvider() string { return providerOr("KMS_PROVIDER", "aws") }

// BlobProvider returns the selected blob provider ("s3" | "filesystem").
// Default s3.
func BlobProvider() string { return providerOr("BLOB_PROVIDER", "s3") }

// EventBusProvider returns the selected event bus ("kafka" | "nats").
// Default kafka.
func EventBusProvider() string { return providerOr("EVENTBUS_PROVIDER", "kafka") }

// CodecProvider returns the selected codec ("avro" | "json"). Default avro.
func CodecProvider() string { return providerOr("CODEC_PROVIDER", "avro") }

// TasksProvider returns the selected task scheduler ("local" | "gcloud").
// Default local — self-host runs the in-process Postgres dispatcher and needs
// no GCP Cloud Tasks.
func TasksProvider() string { return providerOr("TASKS_PROVIDER", "local") }

// BillingProvider returns the selected billing backend ("none" | "stripe").
// Default none — self-host boots without Stripe and unlocks all features.
func BillingProvider() string { return providerOr("BILLING_PROVIDER", "none") }

// CaptchaProvider returns the selected captcha backend ("turnstile" | "none").
// Explicit CAPTCHA_PROVIDER wins; otherwise it's "turnstile" only when a
// TURNSTILE_SECRET is present, so a self-host that sets no secret runs with
// captcha off instead of a broken verify.
func CaptchaProvider() string {
	if v := os.Getenv("CAPTCHA_PROVIDER"); v != "" {
		return v
	}
	if os.Getenv("TURNSTILE_SECRET") == "" {
		return "none"
	}
	return "turnstile"
}

// AWSNeeded reports whether any AWS-backed provider is selected, so a fully
// local deployment (KMS_PROVIDER=local + BLOB_PROVIDER=filesystem) can skip
// loading the AWS SDK config entirely and never needs AWS_REGION / credentials.
func AWSNeeded() bool {
	return KMSProvider() == "aws" || BlobProvider() == "s3"
}

func providerOr(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}
