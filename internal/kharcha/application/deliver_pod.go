package application

// e-POD delivery artifacts write, extracted from internal/handlers/kharcha.go
// (handlers-end-state ADR, Phase 2). The handler keeps the multipart parsing,
// driver authorization and response JSON; this file keeps the legacy
// best-effort trips UPDATE that stores the signature and delivery
// discrepancies after delivery has already succeeded.

import (
	"context"
	"database/sql"
)

// PODArtifacts carries the optional e-POD side-channel fields.
type PODArtifacts struct {
	SignatureData  string
	QuantityShort  float64
	DamageQty      float64
	RefusalReason  string
	ConsigneeName  string
	ConsigneePhone string
	ScanValue      string
}

// UpdateTripPODArtifacts persists the e-POD signature/discrepancy columns with
// the legacy COALESCE/NULLIF/zero-guard semantics: empty strings keep the
// stored value, zero quantities keep the stored value, and the scan value only
// overwrites when non-empty. Contractually best-effort: the legacy handler
// ignored the error because delivery had already succeeded.
func UpdateTripPODArtifacts(ctx context.Context, db *sql.DB, tripID string, a PODArtifacts) error {
	if db == nil {
		return nil
	}
	_, err := db.ExecContext(ctx,
		`UPDATE trips SET pod_signature_data = COALESCE(NULLIF($1,''), pod_signature_data),
		 pod_quantity_short = CASE WHEN $2 != 0 THEN $3 ELSE pod_quantity_short END,
		 pod_damage_qty = CASE WHEN $4 != 0 THEN $5 ELSE pod_damage_qty END,
		 pod_refusal_reason = COALESCE(NULLIF($6,''), pod_refusal_reason),
		 pod_consignee_name = COALESCE(NULLIF($7,''), pod_consignee_name),
		 pod_consignee_phone = COALESCE(NULLIF($8,''), pod_consignee_phone),
		 pod_scan_value = CASE WHEN $9 != '' THEN $10 ELSE pod_scan_value END
		 WHERE id = $11`,
		a.SignatureData, a.QuantityShort, a.QuantityShort, a.DamageQty, a.DamageQty,
		a.RefusalReason, a.ConsigneeName, a.ConsigneePhone, a.ScanValue, a.ScanValue, tripID)
	return err
}
