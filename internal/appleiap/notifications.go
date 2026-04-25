package appleiap

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SignedPayloadEnvelope struct {
	SignedPayload string `json:"signedPayload"`
}

type NotificationPayload struct {
	NotificationUUID string `json:"notificationUUID"`
	NotificationType string `json:"notificationType"`
	Subtype          string `json:"subtype,omitempty"`
	Version          string `json:"version,omitempty"`
	SignedDate       int64  `json:"signedDate,omitempty"`

	Data NotificationData `json:"data"`
}

type NotificationData struct {
	Environment           string `json:"environment,omitempty"` // Sandbox|Production
	AppAppleID            int64  `json:"appAppleId,omitempty"`
	BundleID              string `json:"bundleId,omitempty"`
	BundleVersion         string `json:"bundleVersion,omitempty"`
	SignedTransactionInfo string `json:"signedTransactionInfo,omitempty"`
	SignedRenewalInfo     string `json:"signedRenewalInfo,omitempty"`
}

var ErrMissingNotificationUUID = errors.New("missing notificationUUID")

func VerifyAndDecodeNotification(signedPayload string) (*NotificationPayload, error) {
	b, err := VerifyJWS(signedPayload)
	if err != nil {
		return nil, err
	}
	var p NotificationPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSignedPayload, err)
	}
	if strings.TrimSpace(p.NotificationUUID) == "" {
		return nil, ErrMissingNotificationUUID
	}
	return &p, nil
}
