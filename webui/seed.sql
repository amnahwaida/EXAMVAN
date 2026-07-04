-- ============================================================
-- Seed Data: Realistis dengan berbagai state ujian
-- ============================================================

-- Hapus data lama
DELETE FROM student_access_logs;
DELETE FROM submissions;
DELETE FROM exam_pengawas;
DELETE FROM exams;
DELETE FROM admin_users WHERE username != 'superadmin';
DELETE FROM instansi;

-- Reset sequences
SELECT setval('instansi_id_seq', COALESCE((SELECT MAX(id) FROM instansi), 1));
ALTER SEQUENCE exams_id_seq RESTART WITH 1;
ALTER SEQUENCE exam_pengawas_id_seq RESTART WITH 1;
ALTER SEQUENCE submissions_id_seq RESTART WITH 1;
ALTER SEQUENCE student_access_logs_id_seq RESTART WITH 1;

-- ============================================================
-- Instansi & Users
-- ============================================================
INSERT INTO instansi (id, name) VALUES (1, 'SMA Negeri 1 Jakarta');
INSERT INTO instansi (id, name) VALUES (2, 'SMK Bina Bangsa');

\set pw '\$2b\$12\$I7qRIkyfrnTzcSk23/zKBuNGU.nFySmpeJ1sv5xiofLwpIIZ58e6G'

INSERT INTO admin_users (username, password_hash, status, instansi, instansi_id, role, max_exams, max_pdf_size)
VALUES
('guru.sma1', :'pw', 'active', 'SMA Negeri 1 Jakarta', 1, '["guru"]', 20, 10485760),
('pengawas.sma1', :'pw', 'active', 'SMA Negeri 1 Jakarta', 1, '["pengawas"]', 50, 52428800),
('guru.smk', :'pw', 'active', 'SMK Bina Bangsa', 2, '["guru"]', 20, 10485760);

-- ============================================================
-- Helper Functions
-- ============================================================
CREATE OR REPLACE FUNCTION seed_rand_token() RETURNS TEXT LANGUAGE plpgsql AS $func$
DECLARE
  chars TEXT := 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  result TEXT := '';
BEGIN
  FOR i IN 1..8 LOOP result := result || substr(chars, floor(random() * 36)::int + 1, 1); END LOOP;
  RETURN result;
END $func$;

CREATE OR REPLACE FUNCTION seed_rand_name(seq INT) RETURNS TEXT LANGUAGE plpgsql AS $func$
DECLARE
  first_names TEXT[] := ARRAY['Ahmad','Budi','Citra','Dewi','Eko','Fitri','Gilang','Hana','Irfan','Joko','Kartika','Lukman','Maya','Nanda','Okta'];
  last_names TEXT[] := ARRAY['Pratama','Wijaya','Kusuma','Nugroho','Hidayat','Santoso','Saputra','Wati','Sari','Permadi'];
BEGIN
  RETURN first_names[1 + ((seq * 7) % array_length(first_names, 1))] || ' ' || last_names[1 + ((seq * 13) % array_length(last_names, 1))];
END $func$;

CREATE OR REPLACE FUNCTION seed_rand_mac(seq INT) RETURNS TEXT LANGUAGE plpgsql AS $func$
BEGIN
  RETURN '02:00:00:' || lpad(to_hex(seq / 256), 2, '0') || ':' || lpad(to_hex(seq % 256), 2, '0') || ':' || lpad(to_hex((seq * 7) % 256), 2, '0');
END $func$;

-- ============================================================
-- Generator
-- ============================================================
DO $$
DECLARE
  q_json CONSTANT TEXT := '[{"id":"q1","type":"single_choice","question":"1+1?","options":["1","2","3","4"],"key":"B","weight":1}]';
  id_fields CONSTANT TEXT := '[{"key":"student_name","label":"Nama"},{"key":"student_class","label":"Kelas"},{"key":"exam_number","label":"NISN"}]';
  
  e_id INT; sub_id INT; tkn TEXT; mac TEXT;
  s_name TEXT; s_class TEXT; s_nisn TEXT;
  id_data TEXT; wrong_id_data TEXT;
  now_ts TIMESTAMPTZ := NOW();
BEGIN

  -- ==========================================================
  -- 1. Ujian Selesai (SMA 1)
  -- ==========================================================
  tkn := seed_rand_token();
  INSERT INTO exams (name, file_path, size_bytes, token, active_token, questions_json, status, created_by, identity_fields, panel_color, start_time, end_time, exam_started_at)
  VALUES ('Penilaian Tengah Semester Fisika', 'dummy.pdf', 1000, tkn, tkn, q_json, 'active', 
    (SELECT id FROM admin_users WHERE username='guru.sma1'), id_fields, '#3B82F6', now_ts - interval '2 days', now_ts - interval '1 day', now_ts - interval '2 days')
  RETURNING id INTO e_id;
  
  INSERT INTO exam_pengawas (exam_id, user_id) VALUES (e_id, (SELECT id FROM admin_users WHERE username='pengawas.sma1'));

  FOR i IN 1..10 LOOP
    s_name := seed_rand_name(i); s_class := 'XI IPA 1'; s_nisn := '1000' || i;
    mac := seed_rand_mac(i);
    id_data := '{"student_name":"' || s_name || '","student_class":"' || s_class || '","exam_number":"' || s_nisn || '"}';
    
    INSERT INTO submissions (exam_id, student_name, exam_number, student_class, score, start_time, mac_address, identity_data, created_at)
    VALUES (e_id, s_name, s_nisn, s_class, 80, (now_ts - interval '1 day - 2 hours')::text, mac, id_data, now_ts - interval '1 day - 1 hour')
    RETURNING id INTO sub_id;
    
    INSERT INTO student_access_logs (exam_id, submission_id, student_identifier, student_name, exam_number, student_class, event, ip_address, identity_data, created_at)
    VALUES (e_id, sub_id, mac, s_name, s_nisn, s_class, 'login', '192.168.1.10' || i, id_data, now_ts - interval '1 day - 2 hours'),
           (e_id, sub_id, mac, s_name, s_nisn, s_class, 'logout', '192.168.1.10' || i, id_data, now_ts - interval '1 day - 1 hour');
  END LOOP;

  -- ==========================================================
  -- 2. Ujian Sedang Berlangsung (SMA 1) - DENGAN PERUBAHAN IDENTITAS
  -- ==========================================================
  tkn := seed_rand_token();
  INSERT INTO exams (name, file_path, size_bytes, token, active_token, questions_json, status, created_by, identity_fields, panel_color, start_time, end_time, exam_started_at, security_level, strict_mode)
  VALUES ('Penilaian Akhir Tahun Matematika', 'dummy.pdf', 2000, tkn, tkn, q_json, 'active', 
    (SELECT id FROM admin_users WHERE username='guru.sma1'), id_fields, '#EF4444', now_ts - interval '30 minutes', now_ts + interval '2 hours', now_ts - interval '30 minutes', 'high', 1)
  RETURNING id INTO e_id;
  
  INSERT INTO exam_pengawas (exam_id, user_id) VALUES (e_id, (SELECT id FROM admin_users WHERE username='pengawas.sma1'));

  -- Siswa normal
  FOR i IN 11..15 LOOP
    s_name := seed_rand_name(i); s_class := 'XI IPS 2'; s_nisn := '2000' || i;
    mac := seed_rand_mac(i);
    id_data := '{"student_name":"' || s_name || '","student_class":"' || s_class || '","exam_number":"' || s_nisn || '"}';
    
    INSERT INTO submissions (exam_id, student_name, exam_number, student_class, score, start_time, mac_address, identity_data, created_at)
    VALUES (e_id, s_name, s_nisn, s_class, NULL, (now_ts - interval '25 minutes')::text, mac, id_data, now_ts - interval '25 minutes')
    RETURNING id INTO sub_id;
    
    INSERT INTO student_access_logs (exam_id, submission_id, student_identifier, student_name, exam_number, student_class, event, ip_address, identity_data, created_at)
    VALUES (e_id, sub_id, mac, s_name, s_nisn, s_class, 'login', '192.168.1.50', id_data, now_ts - interval '25 minutes'),
           (e_id, sub_id, mac, s_name, s_nisn, s_class, 'heartbeat', '192.168.1.50', id_data, now_ts - interval '10 minutes'),
           (e_id, sub_id, mac, s_name, s_nisn, s_class, 'heartbeat', '192.168.1.50', id_data, now_ts - interval '2 minutes');
  END LOOP;

  -- Siswa yang mengubah identitas (Salah ketik kelas lalu diperbaiki)
  s_name := 'Budi Santoso'; s_nisn := '200099'; mac := seed_rand_mac(99);
  wrong_id_data := '{"student_name":"Budi Santoso","student_class":"XI SALAH","exam_number":"200099"}';
  id_data := '{"student_name":"Budi Santoso","student_class":"XI IPS 2","exam_number":"200099"}';
  
  INSERT INTO submissions (exam_id, student_name, exam_number, student_class, score, start_time, mac_address, identity_data, created_at)
  VALUES (e_id, s_name, s_nisn, 'XI IPS 2', NULL, (now_ts - interval '20 minutes')::text, mac, id_data, now_ts - interval '20 minutes')
  RETURNING id INTO sub_id;
  
  INSERT INTO student_access_logs (exam_id, submission_id, student_identifier, student_name, exam_number, student_class, event, ip_address, identity_data, created_at)
  VALUES 
    -- 20 menit lalu login dengan kelas salah
    (e_id, sub_id, mac, s_name, s_nisn, 'XI SALAH', 'login', '10.0.0.5', wrong_id_data, now_ts - interval '20 minutes'),
    (e_id, sub_id, mac, s_name, s_nisn, 'XI SALAH', 'heartbeat', '10.0.0.5', wrong_id_data, now_ts - interval '18 minutes'),
    -- 15 menit lalu ia mengubah identitas (re-login dengan kelas benar)
    (e_id, sub_id, mac, s_name, s_nisn, 'XI IPS 2', 'login', '10.0.0.5', id_data, now_ts - interval '15 minutes'),
    (e_id, sub_id, mac, s_name, s_nisn, 'XI IPS 2', 'heartbeat', '10.0.0.5', id_data, now_ts - interval '5 minutes');

  -- Siswa belum mulai
  INSERT INTO submissions (exam_id, student_name, exam_number, student_class, score, start_time, mac_address, identity_data, created_at)
  VALUES (e_id, 'Citra Kirana', '200100', 'XI IPS 2', NULL, NULL, '', NULL, now_ts);

  -- ==========================================================
  -- 3. Ujian Belum Dimulai (SMK Bina Bangsa)
  -- ==========================================================
  tkn := seed_rand_token();
  INSERT INTO exams (name, file_path, size_bytes, token, active_token, questions_json, status, created_by, identity_fields, panel_color, start_time, end_time)
  VALUES ('Ujian Praktik Kejuruan TKJ', 'dummy.pdf', 3000, tkn, tkn, q_json, 'active', 
    (SELECT id FROM admin_users WHERE username='guru.smk'), id_fields, '#10B981', now_ts + interval '2 days', now_ts + interval '3 days');

END $$;

DROP FUNCTION seed_rand_token();
DROP FUNCTION seed_rand_name(INT);
DROP FUNCTION seed_rand_mac(INT);
