-- PG port of 00090_trip_pod_otp.sql | status: NEEDS-REVIEW | flags: HAS-DQUOTE | reviewed: YES
-- +goose Up
-- e-POD OTP verification (finally real): a 6-digit code generated at dispatch,
-- shown to the operator in the trip view, verified at delivery. Until an SMS
-- channel is configured the operator relays the code to the consignee by
-- phone. pod_otp is stored in the clear deliberately — the code's value is
-- "something the consignee says back", scoped to one trip with a 48h expiry.

ALTER TABLE trips ADD COLUMN pod_otp TEXT;
ALTER TABLE trips ADD COLUMN pod_otp_expires_at TEXT;
-- +goose Down
ALTER TABLE trips DROP COLUMN pod_otp_expires_at;
ALTER TABLE trips DROP COLUMN pod_otp;
