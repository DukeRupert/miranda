package mail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client sends email via the Postmark API.
type Client struct {
	serverToken string
	from        string
	to          string
}

// NewClient creates a Postmark mail client.
func NewClient(serverToken, from, to string) *Client {
	return &Client{
		serverToken: serverToken,
		from:        from,
		to:          to,
	}
}

// Message represents a contact form submission to be sent as email.
type Message struct {
	Name    string
	Email   string
	Subject string
	Body    string
}

type postmarkRequest struct {
	From     string `json:"From"`
	To       string `json:"To"`
	Subject  string `json:"Subject"`
	TextBody string `json:"TextBody"`
	ReplyTo  string `json:"ReplyTo"`
}

type postmarkError struct {
	ErrorCode int    `json:"ErrorCode"`
	Message   string `json:"Message"`
}

// Send delivers a message through the Postmark API.
func (c *Client) Send(msg Message) error {
	body := postmarkRequest{
		From:     c.from,
		To:       c.to,
		Subject:  msg.Subject,
		TextBody: fmt.Sprintf("From: %s <%s>\n\n%s", msg.Name, msg.Email, msg.Body),
		ReplyTo:  msg.Email,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal postmark request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.postmarkapp.com/email", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create postmark request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Postmark-Server-Token", c.serverToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("postmark request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	var pmErr postmarkError
	if json.Unmarshal(respBody, &pmErr) == nil && pmErr.Message != "" {
		return fmt.Errorf("postmark error %d: %s", pmErr.ErrorCode, pmErr.Message)
	}
	return fmt.Errorf("postmark returned status %d", resp.StatusCode)
}
