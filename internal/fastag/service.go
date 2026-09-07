package fastag

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	intFastag "transport-app/internal/integration/fastag"
)

// Config holds settings for FASTag operations.
type Config struct {
	AutoKharcha bool
	MerchantID  string
	Provider    string
}

// LoadConfig reads FASTag configuration from company_config.
func LoadConfig(db *sql.DB) Config {
	cfg := Config{
		AutoKharcha: true,
		Provider:    "MOCK",
	}
	if db == nil {
		return cfg
	}

	rows, err := db.Query(`SELECT key, value FROM company_config WHERE key IN ('fastag_auto_kharcha', 'fastag_merchant_id', 'fastag_provider')`)
	if err != nil {
		return cfg
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			switch k {
			case "fastag_auto_kharcha":
				v = strings.ToLower(strings.TrimSpace(v))
				cfg.AutoKharcha = (v == "true" || v == "1" || v == "yes")
			case "fastag_merchant_id":
				cfg.MerchantID = v
			case "fastag_provider":
				cfg.Provider = v
			}
		}
	}
	return cfg
}

// TagRecord represents a FASTag wallet tag row.
type TagRecord struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	VehicleID     *string   `json:"vehicle_id,omitempty"`
	TagID         string    `json:"tag_id"`
	VehicleNumber string    `json:"vehicle_number"`
	Issuer        string    `json:"issuer"`
	TagClass      string    `json:"tag_class"`
	Balance       float64   `json:"balance"`
	Status        string    `json:"status"`
	LastSync      time.Time `json:"last_sync"`
	LastSyncStr   string    `json:"last_sync_str"`
}

// TransactionRecord represents a row in fastag_transactions.
type TransactionRecord struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	TagID         string    `json:"tag_id"`
	VehicleID     *string   `json:"vehicle_id,omitempty"`
	VehicleNumber string    `json:"vehicle_number"`
	TripID        *string   `json:"trip_id,omitempty"`
	PlazaID       string    `json:"plaza_id"`
	PlazaName     string    `json:"plaza_name"`
	Amount        float64   `json:"amount"`
	TxnTimestamp  time.Time `json:"txn_timestamp"`
	TxnTimeStr    string    `json:"txn_time_str"`
	Status        string    `json:"status"`
	Source        string    `json:"source"`
	Reconciled    bool      `json:"reconciled"`
	KharchaID     *string   `json:"kharcha_id,omitempty"`
}

// FASTagService coordinates wallet queries, transactions, and trip reconciliations.
type FASTagService struct {
	db     *sql.DB
	client intFastag.Client
	config Config
	logger *slog.Logger
}

// NewFASTagService creates a new FASTag service.
func NewFASTagService(db *sql.DB, client intFastag.Client, cfg Config, logger ...*slog.Logger) *FASTagService {
	l := slog.Default()
	if len(logger) > 0 && logger[0] != nil {
		l = logger[0]
	}
	return &FASTagService{
		db:     db,
		client: client,
		config: cfg,
		logger: l,
	}
}

// GetBalance returns the balance for a vehicle or tag.
func (s *FASTagService) GetBalance(ctx context.Context, vehicleNumber string) (*intFastag.Balance, error) {
	bal, err := s.client.GetBalance(ctx, vehicleNumber, "")
	if err != nil {
		return nil, err
	}
	return &bal, nil
}

// DeductToll records a toll transaction and decrements wallet balance.
func (s *FASTagService) DeductToll(ctx context.Context, req intFastag.DeductTollRequest) (*intFastag.TollTransaction, error) {
	txn, err := s.client.DeductToll(ctx, req)
	if err != nil {
		return nil, err
	}
	return &txn, nil
}

// ListTransactions retrieves transactions from DB or client.
func (s *FASTagService) ListTransactions(ctx context.Context, vehicleNumber string, limit int) ([]TransactionRecord, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
		SELECT id, tenant_id, tag_id, vehicle_id, vehicle_number, trip_id, plaza_id, plaza_name,
		       amount, txn_timestamp, status, source, reconciled, kharcha_id
		FROM fastag_transactions
		WHERE (vehicle_number = $1 OR $2 = '')
		ORDER BY txn_timestamp DESC
		LIMIT $3
	`
	rows, err := s.db.QueryContext(ctx, query, vehicleNumber, vehicleNumber, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []TransactionRecord
	for rows.Next() {
		var tr TransactionRecord
		var vehID, tripID, kharchaID sql.NullString
		var reconciledInt int
		err := rows.Scan(
			&tr.ID, &tr.TenantID, &tr.TagID, &vehID, &tr.VehicleNumber, &tripID, &tr.PlazaID, &tr.PlazaName,
			&tr.Amount, &tr.TxnTimestamp, &tr.Status, &tr.Source, &reconciledInt, &kharchaID,
		)
		if err != nil {
			continue
		}
		if vehID.Valid {
			tr.VehicleID = &vehID.String
		}
		if tripID.Valid {
			tr.TripID = &tripID.String
		}
		if kharchaID.Valid {
			tr.KharchaID = &kharchaID.String
		}
		tr.Reconciled = (reconciledInt == 1)
		tr.TxnTimeStr = tr.TxnTimestamp.Format("2006-01-02 15:04:05")
		list = append(list, tr)
	}
	return list, nil
}

// ListTags returns all registered tags in the database.
func (s *FASTagService) ListTags(ctx context.Context) ([]TagRecord, error) {
	query := `
		SELECT id, tenant_id, vehicle_id, tag_id, vehicle_number, issuer, tag_class, balance, status, last_sync
		FROM fastag_tags
		ORDER BY vehicle_number ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []TagRecord
	for rows.Next() {
		var tag TagRecord
		var vehID, vehNum, issuer, tagClass, status sql.NullString
		var lastSync sql.NullTime
		err := rows.Scan(
			&tag.ID, &tag.TenantID, &vehID, &tag.TagID, &vehNum, &issuer, &tagClass, &tag.Balance, &status, &lastSync,
		)
		if err != nil {
			continue
		}
		if vehID.Valid {
			tag.VehicleID = &vehID.String
		}
		tag.VehicleNumber = vehNum.String
		tag.Issuer = issuer.String
		tag.TagClass = tagClass.String
		tag.Status = status.String
		if lastSync.Valid {
			tag.LastSync = lastSync.Time
			tag.LastSyncStr = tag.LastSync.Format("2006-01-02 15:04:05")
		} else {
			tag.LastSyncStr = "-"
		}
		tags = append(tags, tag)
	}
	return tags, nil
}
