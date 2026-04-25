package injection

func NamedLibrary(name string) (ResponseOverride, bool) {
	override, ok := namedErrors[name]
	if !ok {
		return ResponseOverride{}, false
	}

	return ResponseOverride{
		Status: override.Status,
		Body:   cloneAnyMap(override.Body),
	}, true
}

func NamedLibraryEntries() map[string]ResponseOverride {
	entries := make(map[string]ResponseOverride, len(namedErrors))
	for name, override := range namedErrors {
		entries[name] = ResponseOverride{
			Status: override.Status,
			Body:   cloneAnyMap(override.Body),
		}
	}
	return entries
}

var namedErrors = map[string]ResponseOverride{
	"stripe.card_declined": {
		Status: 402,
		Body: map[string]any{
			"error": map[string]any{
				"type":         "card_error",
				"code":         "card_declined",
				"decline_code": "generic_decline",
				"message":      "Your card was declined.",
			},
		},
	},
	"stripe.insufficient_funds": {
		Status: 402,
		Body: map[string]any{
			"error": map[string]any{
				"type":         "card_error",
				"code":         "card_declined",
				"decline_code": "insufficient_funds",
				"message":      "Your card has insufficient funds.",
			},
		},
	},
	"stripe.expired_card": {
		Status: 402,
		Body: map[string]any{
			"error": map[string]any{
				"type":         "card_error",
				"code":         "expired_card",
				"decline_code": "expired_card",
				"message":      "Your card has expired.",
			},
		},
	},
	"stripe.authentication_error": {
		Status: 401,
		Body: map[string]any{
			"error": map[string]any{
				"type":    "invalid_request_error",
				"code":    "authentication_error",
				"message": "Invalid API key provided.",
			},
		},
	},
	"stripe.rate_limit": {
		Status: 429,
		Body: map[string]any{
			"error": map[string]any{
				"type":    "rate_limit_error",
				"code":    "rate_limit",
				"message": "Too many requests hit the API too quickly.",
			},
		},
	},
	"stripe.api_connection_error": {
		Status: 503,
		Body: map[string]any{
			"error": map[string]any{
				"type":    "api_connection_error",
				"code":    "api_connection_error",
				"message": "An error occurred while connecting to Stripe.",
			},
		},
	},
}
