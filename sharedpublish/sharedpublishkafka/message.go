package sharedpublishkafka

import (
	"time"

	ledger "github.com/numary/ledger/pkg/core"
	payments "github.com/numary/payments/pkg/core"
)

const (
	TopicPayments = "payments"

	EventLedgerCommittedTransactions = "COMMITTED_TRANSACTIONS"
	EventLedgerSavedMetadata         = "SAVED_METADATA"
	EventLedgerUpdatedMapping        = "UPDATED_MAPPING"
	EventLedgerRevertedTransaction   = "REVERTED_TRANSACTION"

	EventPaymentsSavedPayment = "SAVED_PAYMENT"
)

type EventLedgerMessage[T any] struct {
	Date    time.Time `json:"date"`
	Type    string    `json:"type"`
	Payload T         `json:"payload"`
	Ledger  string    `json:"ledger"`
}

type CommittedTransactions struct {
	Transactions []ledger.ExpandedTransaction `json:"transactions"`
	// Deprecated (use postCommitVolumes)
	Volumes           ledger.AccountsAssetsVolumes `json:"volumes"`
	PostCommitVolumes ledger.AccountsAssetsVolumes `json:"postCommitVolumes"`
	PreCommitVolumes  ledger.AccountsAssetsVolumes `json:"preCommitVolumes"`
}

type SavedMetadata struct {
	TargetType string          `json:"targetType"`
	TargetID   string          `json:"targetId"`
	Metadata   ledger.Metadata `json:"metadata"`
}

type RevertedTransaction struct {
	RevertedTransaction ledger.ExpandedTransaction `json:"revertedTransaction"`
	RevertTransaction   ledger.ExpandedTransaction `json:"revertTransaction"`
}

type UpdatedMapping struct {
	Mapping ledger.Mapping `json:"mapping"`
}

type EventPaymentsMessage struct {
	Date    time.Time                `json:"date"`
	Type    string                   `json:"type"`
	Payload payments.ComputedPayment `json:"payload"`
}
