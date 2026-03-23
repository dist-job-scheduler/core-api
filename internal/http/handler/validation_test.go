package handler

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-playground/validator/v10"
)

func TestFormatValidationError_Required(t *testing.T) {
	type req struct {
		URL string `validate:"required"`
	}
	err := validator.New().Struct(req{})

	got := formatValidationError(err)
	if !strings.Contains(got, "url is required") {
		t.Errorf("formatValidationError() = %q, want to contain %q", got, "url is required")
	}
}

func TestFormatValidationError_Oneof(t *testing.T) {
	type req struct {
		Method string `validate:"oneof=GET POST"`
	}
	err := validator.New().Struct(req{Method: "INVALID"})

	got := formatValidationError(err)
	if !strings.Contains(got, "method must be one of") {
		t.Errorf("formatValidationError() = %q, want to contain %q", got, "method must be one of")
	}
}

func TestFormatValidationError_URL(t *testing.T) {
	type req struct {
		URL string `validate:"url"`
	}
	err := validator.New().Struct(req{URL: "not-a-url"})

	got := formatValidationError(err)
	if !strings.Contains(got, "url must be a valid URL") {
		t.Errorf("formatValidationError() = %q, want to contain %q", got, "url must be a valid URL")
	}
}

func TestFormatValidationError_MinMax(t *testing.T) {
	type req struct {
		Timeout int `validate:"min=1,max=3600"`
	}
	err := validator.New().Struct(req{Timeout: 0})

	got := formatValidationError(err)
	if !strings.Contains(got, "timeout must be at least 1") {
		t.Errorf("formatValidationError() = %q, want to contain %q", got, "timeout must be at least 1")
	}
}

func TestFormatValidationError_MultipleFields(t *testing.T) {
	type req struct {
		URL    string `validate:"required"`
		Method string `validate:"required"`
	}
	err := validator.New().Struct(req{})

	got := formatValidationError(err)
	if !strings.Contains(got, ";") {
		t.Errorf("formatValidationError() = %q, want multiple errors joined by semicolons", got)
	}
}

func TestFormatValidationError_NonValidatorError(t *testing.T) {
	got := formatValidationError(errors.New("unexpected EOF"))
	if got != "Invalid request body" {
		t.Errorf("formatValidationError() = %q, want %q", got, "Invalid request body")
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"URL", "url"},
		{"TimeoutSeconds", "timeout_seconds"},
		{"Method", "method"},
		{"MaxRetries", "max_retries"},
		{"CronExpr", "cron_expr"},
		{"WebhookURL", "webhook_url"},
		{"IDKey", "id_key"},
	}
	for _, tt := range tests {
		got := toSnakeCase(tt.input)
		if got != tt.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
