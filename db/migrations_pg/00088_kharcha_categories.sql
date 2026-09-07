-- PG port of 00088_kharcha_categories.sql | status: MANUAL | flags: MANUAL-REBUILD | reviewed: YES
-- Native PG port: only expense_type/category CHECK lists widen — DROP/ADD
-- CONSTRAINT suffices, no rebuild, dependents kept. (Constraint names are the
-- PG auto-names from the anonymous CHECKs in the ported 00031/00032/00043.)
-- +goose Up
ALTER TABLE driver_expenses DROP CONSTRAINT driver_expenses_expense_type_check;
ALTER TABLE driver_expenses ADD CONSTRAINT driver_expenses_expense_type_check CHECK (expense_type IN ('fuel', 'toll', 'food', 'repair', 'advance', 'other', 'rto', 'tyre', 'bhatta'));
ALTER TABLE driver_expenses DROP CONSTRAINT driver_expenses_category_check;
ALTER TABLE driver_expenses ADD CONSTRAINT driver_expenses_category_check CHECK (category IN ('advance', 'fuel', 'toll', 'food', 'repair', 'other', 'rto', 'tyre', 'bhatta'));
-- +goose Down
-- Fold rto/tyre/bhatta rows into the nearest original bucket first (lossy, intentional).
UPDATE driver_expenses SET expense_type = CASE expense_type WHEN 'rto' THEN 'repair' WHEN 'tyre' THEN 'repair' WHEN 'bhatta' THEN 'advance' ELSE expense_type END WHERE expense_type IN ('rto', 'tyre', 'bhatta');
UPDATE driver_expenses SET category = 'other' WHERE category IN ('rto', 'tyre', 'bhatta');
ALTER TABLE driver_expenses DROP CONSTRAINT driver_expenses_expense_type_check;
ALTER TABLE driver_expenses ADD CONSTRAINT driver_expenses_expense_type_check CHECK (expense_type IN ('fuel', 'toll', 'food', 'repair', 'advance'));
ALTER TABLE driver_expenses DROP CONSTRAINT driver_expenses_category_check;
ALTER TABLE driver_expenses ADD CONSTRAINT driver_expenses_category_check CHECK (category IN ('advance', 'fuel', 'toll', 'food', 'repair', 'other'));
