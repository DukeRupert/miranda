package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/firefly-software-mt/advanced-template/internal/mail"
	"github.com/firefly-software-mt/advanced-template/internal/view"
)

// Contact handles GET /contact and renders the contact form.
func Contact() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := view.ContactPage(nil, nil, false).Render(r.Context(), w); err != nil {
			slog.Error("render error", "err", err)
		}
	}
}

// ContactSubmit handles POST /contact, validates input, and sends a message.
func ContactSubmit(mailer *mail.Client, turnstileSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		values := map[string]string{
			"name":    strings.TrimSpace(r.FormValue("name")),
			"email":   strings.TrimSpace(r.FormValue("email")),
			"message": strings.TrimSpace(r.FormValue("message")),
		}

		errors := validate(values)

		if len(errors) > 0 {
			if err := view.ContactForm(errors, values, false).Render(r.Context(), w); err != nil {
				slog.Error("render error", "err", err)
			}
			return
		}

		// Verify Turnstile token
		if turnstileSecret != "" {
			token := r.FormValue("cf-turnstile-response")
			if !verifyTurnstile(turnstileSecret, token, r.RemoteAddr) {
				errors = map[string]string{"form": "Verification failed. Please try again."}
				if err := view.ContactForm(errors, values, false).Render(r.Context(), w); err != nil {
					slog.Error("render error", "err", err)
				}
				return
			}
		}

		if mailer != nil {
			msg := mail.Message{
				Name:    values["name"],
				Email:   values["email"],
				Subject: fmt.Sprintf("Contact form: %s", values["name"]),
				Body:    values["message"],
			}
			if err := mailer.Send(msg); err != nil {
				slog.Error("postmark send error", "err", err)
				errors = map[string]string{"form": "Failed to send message. Please try again."}
				if err := view.ContactForm(errors, values, false).Render(r.Context(), w); err != nil {
					slog.Error("render error", "err", err)
				}
				return
			}
		}

		if err := view.ContactForm(nil, nil, true).Render(r.Context(), w); err != nil {
			slog.Error("render error", "err", err)
		}
	}
}

// validate checks contact form values and returns a map of field errors.
func validate(values map[string]string) map[string]string {
	errors := make(map[string]string)

	if values["name"] == "" {
		errors["name"] = "Name is required."
	}
	if values["email"] == "" {
		errors["email"] = "Email is required."
	} else if !strings.Contains(values["email"], "@") {
		errors["email"] = "Enter a valid email address."
	}
	if values["message"] == "" {
		errors["message"] = "Message is required."
	}

	return errors
}

// verifyTurnstile checks a Turnstile token against the Cloudflare API.
func verifyTurnstile(secret, token, remoteIP string) bool {
	resp, err := http.PostForm("https://challenges.cloudflare.com/turnstile/v0/siteverify", url.Values{
		"secret":   {secret},
		"response": {token},
		"remoteip": {remoteIP},
	})
	if err != nil {
		slog.Error("turnstile verify request failed", "err", err)
		return false
	}
	defer resp.Body.Close()

	var result struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("turnstile verify decode failed", "err", err)
		return false
	}

	if !result.Success {
		slog.Warn("turnstile verification failed")
	}
	return result.Success
}
