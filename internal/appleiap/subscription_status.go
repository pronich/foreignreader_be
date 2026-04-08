package appleiap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SubscriptionStatusResponse is the JSON body from:
// GET /inApps/v1/subscriptions/{originalTransactionId}
// See: https://developer.apple.com/documentation/appstoreserverapi/statusresponse
type SubscriptionStatusResponse struct {
	Environment string                    `json:"environment,omitempty"`
	BundleID    string                    `json:"bundleId,omitempty"`
	Data        []SubscriptionGroupStatus `json:"data"`
}

type SubscriptionGroupStatus struct {
	SubscriptionGroupIdentifier string                      `json:"subscriptionGroupIdentifier,omitempty"`
	LastTransactions            []SubscriptionLastTransaction `json:"lastTransactions"`
}

// SubscriptionLastTransaction is one row in lastTransactions (includes Apple's status enum).
type SubscriptionLastTransaction struct {
	OriginalTransactionID string `json:"originalTransactionId"`
	Status                int    `json:"status"` // 1 active, 2 expired, 3 billing retry, 4 grace, 5 revoked
	SignedTransactionInfo string `json:"signedTransactionInfo"`
	SignedRenewalInfo     string `json:"signedRenewalInfo,omitempty"`
}

// GetSubscriptionStatuses calls:
// GET /inApps/v1/subscriptions/{originalTransactionId}
func (c *Client) GetSubscriptionStatuses(ctx context.Context, originalTransactionID string) (*SubscriptionStatusResponse, error) {
	ot := strings.TrimSpace(originalTransactionID)
	if ot == "" {
		return nil, errors.New("missing originalTransactionId")
	}
	token, err := c.authToken(time.Now())
	if err != nil {
		return nil, err
	}
	url := c.Env.BaseURL() + "/inApps/v1/subscriptions/" + ot
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apple subscriptions api status=%d", resp.StatusCode)
	}
	var out SubscriptionStatusResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("decode subscription status: %w", err)
	}
	return &out, nil
}

// BestProTransactionPayload picks the latest subscription transaction for proProductID from Apple's status response.
// It prefers the decoded JWS with the greatest expiresDate (milliseconds).
func BestProTransactionPayload(sub *SubscriptionStatusResponse, proProductID string) (*TransactionPayload, error) {
	if sub == nil || len(sub.Data) == 0 {
		return nil, errors.New("empty subscription status")
	}
	pro := strings.TrimSpace(proProductID)
	var best *TransactionPayload
	var bestExpires int64 = -1
	for _, g := range sub.Data {
		for _, lt := range g.LastTransactions {
			if strings.TrimSpace(lt.SignedTransactionInfo) == "" {
				continue
			}
			var p TransactionPayload
			if err := DecodeJWSPayload(lt.SignedTransactionInfo, &p); err != nil {
				continue
			}
			if strings.TrimSpace(p.ProductID) != pro {
				continue
			}
			if p.ExpiresDate > bestExpires {
				bestExpires = p.ExpiresDate
				pc := p
				best = &pc
			}
		}
	}
	if best == nil {
		return nil, errors.New("no matching product in subscription status")
	}
	return best, nil
}

// SubscriptionEnvironment returns normalized environment from status response or empty.
func SubscriptionEnvironment(sub *SubscriptionStatusResponse) string {
	if sub == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(sub.Environment))
}
