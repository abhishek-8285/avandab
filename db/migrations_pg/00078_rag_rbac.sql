-- PG port of 00078_rag_rbac.sql | status: NEEDS-REVIEW | flags: HAS-DQUOTE | reviewed: YES
-- +goose Up
-- Security audit fix M2: RAG API endpoints previously required only
-- authentication. Seed rag:read / rag:write permissions so
-- RequirePermission("rag", ...) gates /api/rag/* (search/stats vs
-- index/reindex/teach/upload).

INSERT INTO permissions (name, description) VALUES
('rag:read', 'Search knowledge base and view index stats'),
('rag:write', 'Teach, index, reindex and upload knowledge base content') ON CONFLICT DO NOTHING;
-- Admin (role id 1) gets both.
INSERT INTO role_permissions (role_id, permission_id)
SELECT 1, id FROM permissions WHERE name IN ('rag:read','rag:write') ON CONFLICT DO NOTHING;
-- Dispatcher (role id 2) gets read-only access.
INSERT INTO role_permissions (role_id, permission_id)
SELECT 2, id FROM permissions WHERE name IN ('rag:read') ON CONFLICT DO NOTHING;
-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN
(SELECT id FROM permissions WHERE name IN ('rag:read','rag:write'));
DELETE FROM permissions WHERE name IN ('rag:read','rag:write');
