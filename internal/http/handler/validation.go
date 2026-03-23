package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
)

// parseLimit parses the "limit" query parameter. Returns 0 (meaning "use
// default") when the parameter is empty. Returns an error when the value is
// present but out of [1, 100].
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		return 0, fmt.Errorf("invalid limit")
	}
	return n, nil
}

// formatValidationError converts Gin/validator errors into user-friendly messages.
// For validator.ValidationErrors, it produces messages like "url is required".
// For other errors (e.g., malformed JSON), it returns a generic message.
func formatValidationError(err error) string {
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		// JSON syntax error or other binding failure.
		return "Invalid request body"
	}

	msgs := make([]string, 0, len(ve))
	for _, fe := range ve {
		field := toSnakeCase(fe.Field())
		msgs = append(msgs, formatFieldError(field, fe))
	}
	return strings.Join(msgs, "; ")
}

func formatFieldError(field string, fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, fe.Param())
	case "min":
		return fmt.Sprintf("%s must be at least %s", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s", field, fe.Param())
	default:
		return fmt.Sprintf("%s is invalid", field)
	}
}

// toSnakeCase converts PascalCase field names to snake_case for API consistency.
// Handles consecutive uppercase letters: "WebhookURL" → "webhook_url".
func toSnakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			// Insert underscore before uppercase if preceded by lowercase,
			// or if this is the start of a new word within an acronym
			// (e.g., the 'U' in "URLParser" → "url_parser").
			if i > 0 {
				prev := runes[i-1]
				nextIsLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
				if prev >= 'a' && prev <= 'z' || (prev >= 'A' && prev <= 'Z' && nextIsLower) {
					b.WriteByte('_')
				}
			}
			b.WriteRune(r + 32) // toLower
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
