package proto

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const (
	MessageSendProcessing = "processing"
	MessageSendDelivered  = "delivered"
	MessageSendFailed     = "failed"
	MessageSendUnknown    = "unknown"
)

// SendTraceID is a bounded diagnostic correlation, never the original message ID.
// Empty IDs carry no cross-process correlation; timings remain useful locally.
func SendTraceID(id string) string {
	if id == "" {
		return "none"
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

// MessageSendState reports an HTTP send's progress to session subscribers.
// Processing only acknowledges hub receipt, not persistence or runtime delivery.
// It never supersedes the session's state or another executing turn. Clients keep
// it per client message ID, independently of session snapshots, and reconcile a
// lost event with the final HTTP response or a retry using the same ID.
// Delivered acknowledges input delivery, not completion of the model's answer.
type MessageSendState struct {
	Type        string    `json:"type"`
	MachineID   string    `json:"machine_id"`
	SessionID   string    `json:"session_id"`
	ClientMsgID string    `json:"client_msg_id"`
	State       string    `json:"state"`
	At          time.Time `json:"at"`
}
