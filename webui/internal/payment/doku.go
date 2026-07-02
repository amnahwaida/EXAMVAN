package payment

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// DokuClient holds integration parameters for DOKU (Jokul Checkout v1)
type DokuClient struct {
	ClientID  string
	SecretKey string
	APIURL    string
}

// NewDokuClient creates a new DokuClient instance
func NewDokuClient(clientID, secretKey, apiURL string) *DokuClient {
	if apiURL == "" {
		apiURL = "https://api-sandbox.doku.com"
	}
	return &DokuClient{
		ClientID:  clientID,
		SecretKey: secretKey,
		APIURL:    apiURL,
	}
}

type DokuOrderRequest struct {
	Order struct {
		InvoiceNumber string `json:"invoice_number"`
		Amount        int64  `json:"amount"`
		CallbackURL   string `json:"callback_url"`
	} `json:"order"`
	Customer struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"customer"`
}

type DokuOrderResponse struct {
	Order struct {
		InvoiceNumber string `json:"invoice_number"`
		Amount        int64  `json:"amount"`
	} `json:"order"`
	Payment struct {
		URL         string `json:"url"`
		ExpiredDate string `json:"expired_date"`
	} `json:"payment"`
	Error struct {
		Message []string `json:"message"`
	} `json:"error"`
}

// CreateCheckout initiates a checkout request to DOKU and returns the payment redirect URL
func (d *DokuClient) CreateCheckout(invoiceNum string, amount int64, callbackURL, customerName, customerEmail string) (string, error) {
	reqBody := DokuOrderRequest{}
	reqBody.Order.InvoiceNumber = invoiceNum
	reqBody.Order.Amount = amount
	reqBody.Order.CallbackURL = callbackURL
	reqBody.Customer.Name = customerName
	reqBody.Customer.Email = customerEmail

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	requestID := uuid.New().String()
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	targetPath := "/checkout/v1/payment"

	digest := CalculateDigest(jsonBytes)
	signature := GenerateDokuSignature(d.ClientID, requestID, timestamp, targetPath, digest, d.SecretKey)

	req, err := http.NewRequest("POST", d.APIURL+targetPath, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Client-Id", d.ClientID)
	req.Header.Set("Request-Id", requestID)
	req.Header.Set("Request-Timestamp", timestamp)
	req.Header.Set("Signature", signature)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("doku api error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var res DokuOrderResponse
	if err := json.Unmarshal(respBytes, &res); err != nil {
		return "", err
	}

	if len(res.Error.Message) > 0 {
		return "", fmt.Errorf("doku response error: %v", res.Error.Message)
	}

	if res.Payment.URL == "" {
		return "", fmt.Errorf("doku response missing payment url: %s", string(respBytes))
	}

	return res.Payment.URL, nil
}

// GenerateDokuSignature generates a DOKU-compliant HMAC-SHA256 signature for requests/notifications
func GenerateDokuSignature(clientID, requestID, timestamp, targetPath, digest, secretKey string) string {
	component := fmt.Sprintf("Client-Id:%s\nRequest-Id:%s\nRequest-Timestamp:%s\nRequest-Target:%s\nDigest:%s",
		clientID, requestID, timestamp, targetPath, digest)

	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(component))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return "HMACSHA256=" + signature
}

// CalculateDigest generates a base64 encoded SHA256 hash of the payload
func CalculateDigest(body []byte) string {
	h := sha256.New()
	h.Write(body)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
