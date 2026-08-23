-- migration: 002_consent_log
-- Audit trail of consent prompts shown to the driver (GDPR/DPDP style).

CREATE TABLE IF NOT EXISTS consent_log (
  purpose TEXT NOT NULL,
  user_response TEXT NOT NULL CHECK(user_response IN ('granted','denied')),
  timestamp TEXT NOT NULL DEFAULT (datetime('now'))
);

-- down: DROP TABLE IF EXISTS consent_log;
