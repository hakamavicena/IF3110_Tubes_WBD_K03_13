-- DEMO ONLY: Kredensial dan data demo untuk pengujian Milestone 1
-- Password semua user demo: password123 (hash bcrypt)

-- 1. Insert user demo jika belum ada
INSERT INTO users (username, password_hash, is_admin, is_active) VALUES
('editor', '$2a$10$jr92yrACh1xNQqF/5GtJD.yeiKE8ebSzOdcB4aq08GSUpxzmzqd/i', false, true),
('viewer', '$2a$10$jr92yrACh1xNQqF/5GtJD.yeiKE8ebSzOdcB4aq08GSUpxzmzqd/i', false, true),
('multi', '$2a$10$jr92yrACh1xNQqF/5GtJD.yeiKE8ebSzOdcB4aq08GSUpxzmzqd/i', false, true),
('outsider', '$2a$10$jr92yrACh1xNQqF/5GtJD.yeiKE8ebSzOdcB4aq08GSUpxzmzqd/i', false, true)
ON CONFLICT (username) DO NOTHING;

-- 2. Insert grup demo
INSERT INTO groups (name) VALUES
('g_view'),
('g_edit')
ON CONFLICT (name) DO NOTHING;

-- 3. Masukkan keanggotaan grup
-- editor -> g_edit
INSERT INTO user_groups (user_id, group_id)
SELECT u.id, g.id FROM users u, groups g
WHERE u.username = 'editor' AND g.name = 'g_edit'
ON CONFLICT DO NOTHING;

-- viewer -> g_view
INSERT INTO user_groups (user_id, group_id)
SELECT u.id, g.id FROM users u, groups g
WHERE u.username = 'viewer' AND g.name = 'g_view'
ON CONFLICT DO NOTHING;

-- multi -> g_view dan g_edit
INSERT INTO user_groups (user_id, group_id)
SELECT u.id, g.id FROM users u, groups g
WHERE u.username = 'multi' AND g.name IN ('g_view', 'g_edit')
ON CONFLICT DO NOTHING;

-- Catatan: admin dan outsider TIDAK dimasukkan ke grup apa pun secara default
