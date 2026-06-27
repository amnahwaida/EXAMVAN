-- ============================================================
-- Seed Data: 2 instansi, masing-masing: 1 operator, 5 guru,
--            3 pengawas, 1 ujian per guru, 40+ siswa per ujian
-- Password semua user: password123
-- ============================================================

-- Hapus data lama
DELETE FROM submissions;
DELETE FROM exam_pengawas;
DELETE FROM student_access_logs;
DELETE FROM exams;
DELETE FROM admin_users;
DELETE FROM instansi;

-- Reset sequences
ALTER SEQUENCE admin_users_id_seq RESTART WITH 1;
ALTER SEQUENCE exams_id_seq RESTART WITH 1;
ALTER SEQUENCE exam_pengawas_id_seq RESTART WITH 1;
ALTER SEQUENCE submissions_id_seq RESTART WITH 1;

-- ============================================================
-- Instansi
-- ============================================================
INSERT INTO instansi (id, name) VALUES (1, 'SMP Negeri 1 Jakarta');
INSERT INTO instansi (id, name) VALUES (2, 'SMA Negeri 2 Bandung');

-- ============================================================
-- Admin Users (bcrypt hash for "password123")
-- ============================================================
\set pw '\$2b\$12\$I7qRIkyfrnTzcSk23/zKBuNGU.nFySmpeJ1sv5xiofLwpIIZ58e6G'

INSERT INTO admin_users (username, password_hash, status, instansi, instansi_id, role, max_exams, max_pdf_size)
VALUES ('superadmin', :'pw', 'active', '', NULL, '["superadmin"]', 9999, 999999999);

INSERT INTO admin_users (username, password_hash, status, instansi, instansi_id, role, max_exams, max_pdf_size)
VALUES ('operator.smp1', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["operator"]', 100, 52428800);

INSERT INTO admin_users (username, password_hash, status, instansi, instansi_id, role, max_exams, max_pdf_size)
VALUES
('guru.smp1.a', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["guru"]', 20, 10485760),
('guru.smp1.b', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["guru"]', 20, 10485760),
('guru.smp1.c', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["guru"]', 20, 10485760),
('guru.smp1.d', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["guru"]', 20, 10485760),
('guru.smp1.e', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["guru"]', 20, 10485760);

INSERT INTO admin_users (username, password_hash, status, instansi, instansi_id, role, max_exams, max_pdf_size)
VALUES
('pengawas.smp1.a', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["pengawas"]', 50, 52428800),
('pengawas.smp1.b', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["pengawas"]', 50, 52428800),
('pengawas.smp1.c', :'pw', 'active', 'SMP Negeri 1 Jakarta', 1, '["pengawas"]', 50, 52428800);

INSERT INTO admin_users (username, password_hash, status, instansi, instansi_id, role, max_exams, max_pdf_size)
VALUES ('operator.sma2', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["operator"]', 100, 52428800);

INSERT INTO admin_users (username, password_hash, status, instansi, instansi_id, role, max_exams, max_pdf_size)
VALUES
('guru.sma2.a', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["guru"]', 20, 10485760),
('guru.sma2.b', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["guru"]', 20, 10485760),
('guru.sma2.c', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["guru"]', 20, 10485760),
('guru.sma2.d', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["guru"]', 20, 10485760),
('guru.sma2.e', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["guru"]', 20, 10485760);

INSERT INTO admin_users (username, password_hash, status, instansi, instansi_id, role, max_exams, max_pdf_size)
VALUES
('pengawas.sma2.a', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["pengawas"]', 50, 52428800),
('pengawas.sma2.b', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["pengawas"]', 50, 52428800),
('pengawas.sma2.c', :'pw', 'active', 'SMA Negeri 2 Bandung', 2, '["pengawas"]', 50, 52428800);

-- ============================================================
-- Helper functions for generating random data
-- ============================================================
CREATE OR REPLACE FUNCTION seed_rand_token() RETURNS TEXT LANGUAGE plpgsql AS $func$
DECLARE
  chars TEXT := 'ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  result TEXT := '';
BEGIN
  FOR i IN 1..8 LOOP
    result := result || substr(chars, floor(random() * 36)::int + 1, 1);
  END LOOP;
  RETURN result;
END $func$;

CREATE OR REPLACE FUNCTION seed_rand_name(seq INT) RETURNS TEXT LANGUAGE plpgsql AS $func$
DECLARE
  first_names TEXT[] := ARRAY['Ahmad','Budi','Citra','Dewi','Eko','Fitri','Gilang','Hana','Irfan','Joko','Kartika','Lukman','Maya','Nanda','Okta','Putri','Qori','Rizky','Sari','Teguh','Umar','Vina','Wahyu','Yoga','Zahra','Adi','Bayu','Cahya','Dina','Eka','Fajar','Gita','Hendra','Intan','Jaya','Kurnia','Lina','Mega','Nina','Oscar','Puspita','Rama','Sinta','Tri','Utami','Vega','Wulan','Yanti','Zain'];
  last_names TEXT[] := ARRAY['Abdullah','Pratama','Wijaya','Kusuma','Nugroho','Hidayat','Santoso','Saputra','Wati','Sari','Permadi','Anggraini','Setiawan','Gunawan','Hartono','Susanti','Purnomo','Utami','Rahmawati','Suryadi','Maulana','Hakim','Mustofa','Fadillah','Rachman','Pangestu','Lestari','Handayani','Wibowo','Cahyono'];
BEGIN
  RETURN first_names[1 + ((seq * 7 + seq * seq) % array_length(first_names, 1))]
    || ' ' || last_names[1 + ((seq * 13 + seq * 3) % array_length(last_names, 1))];
END $func$;

CREATE OR REPLACE FUNCTION seed_gen_answers(score INT) RETURNS TEXT LANGUAGE plpgsql AS $func$
DECLARE
  ans TEXT := '[]';
  answer_key TEXT := 'BCCCCCCBAA';
  correct_letter TEXT;
BEGIN
  FOR k IN 1..10 LOOP
    correct_letter := CASE substr(answer_key, k, 1)
      WHEN 'A' THEN 'A' WHEN 'B' THEN 'B' WHEN 'C' THEN 'C' WHEN 'D' THEN 'D' END;
    IF random() < score::float / 100 THEN
      ans := (ans::jsonb || jsonb_build_object('question_id', 'q' || k, 'answer', correct_letter))::text;
    ELSE
      ans := (ans::jsonb || jsonb_build_object('question_id', 'q' || k, 'answer',
        CASE floor(random() * 4)::int WHEN 0 THEN 'A' WHEN 1 THEN 'B' WHEN 2 THEN 'C' WHEN 3 THEN 'D' END))::text;
    END IF;
  END LOOP;
  RETURN ans;
END $func$;

-- ============================================================
-- Exams & Submissions
-- ============================================================
DO $$
DECLARE
  questions_json CONSTANT TEXT := '[
    {"id":"q1","type":"single_choice","question":"Hitunglah 25 + 17 = ...","options":["32","42","52","62"],"key":"B","weight":1},
    {"id":"q2","type":"single_choice","question":"Manakah yang termasuk bilangan prima?","options":["12","17","21","27"],"key":"B","weight":1},
    {"id":"q3","type":"single_choice","question":"Ibu membeli 3 kg apel dan 2 kg jeruk. Harga 1 kg apel Rp15.000 dan 1 kg jeruk Rp12.000. Berapa total belanja ibu?","options":["Rp65.000","Rp67.000","Rp69.000","Rp71.000"],"key":"C","weight":1},
    {"id":"q4","type":"single_choice","question":"Sebuah persegi panjang memiliki panjang 12 cm dan lebar 8 cm. Luasnya adalah ...","options":["86 cm²","92 cm²","96 cm²","100 cm²"],"key":"C","weight":1},
    {"id":"q5","type":"single_choice","question":"20% dari 250 adalah ...","options":["30","40","50","60"],"key":"C","weight":1},
    {"id":"q6","type":"single_choice","question":"KPK dari 6 dan 8 adalah ...","options":["12","18","24","48"],"key":"C","weight":1},
    {"id":"q7","type":"single_choice","question":"Akar kuadrat dari 144 adalah ...","options":["10","11","12","13"],"key":"C","weight":1},
    {"id":"q8","type":"single_choice","question":"Volume kubus dengan rusuk 5 cm adalah ...","options":["100 cm³","125 cm³","150 cm³","175 cm³"],"key":"B","weight":1},
    {"id":"q9","type":"single_choice","question":"Skala 1:100.000 artinya 1 cm pada peta sama dengan ...","options":["1 km","10 km","100 km","1000 km"],"key":"A","weight":1},
    {"id":"q10","type":"single_choice","question":"Urutan pecahan 2/3, 3/4, 5/6 dari terkecil adalah ...","options":["2/3, 3/4, 5/6","3/4, 5/6, 2/3","5/6, 3/4, 2/3","2/3, 5/6, 3/4"],"key":"A","weight":1}]';
  identity_fields_json CONSTANT TEXT := '[
    {"key":"student_name","label":"Nama","required":true},
    {"key":"exam_number","label":"Nomor Ujian","required":true},
    {"key":"student_class","label":"Kelas","required":true}]';

  exam_id INT;
  guru_id INT;
  tkn TEXT;
  exam_name TEXT;
  i INT;
  j INT;
  score INT;
  class_name TEXT;
BEGIN
  -- === SMP Negeri 1 Jakarta (guru IDs 2-6) ===
  FOR guru_id IN 2..6 LOOP
    tkn := seed_rand_token();
    WHILE EXISTS (SELECT 1 FROM exams WHERE token = tkn OR active_token = tkn) LOOP
      tkn := seed_rand_token();
    END LOOP;

    exam_name := CASE guru_id
      WHEN 2 THEN 'Ulangan Harian Matematika Kelas 7A'
      WHEN 3 THEN 'Ulangan Harian Bahasa Indonesia Kelas 7B'
      WHEN 4 THEN 'Ulangan Harian IPA Kelas 8A'
      WHEN 5 THEN 'Ulangan Harian IPS Kelas 8B'
      WHEN 6 THEN 'Ulangan Harian Pendidikan Pancasila Kelas 9A'
    END;

    class_name := CASE guru_id
      WHEN 2 THEN '7A' WHEN 3 THEN '7B'
      WHEN 4 THEN '8A' WHEN 5 THEN '8B'
      WHEN 6 THEN '9A'
    END;

    INSERT INTO exams (name, file_path, size_bytes, token, active_token, questions_json, status,
      security_level, strict_mode, public_results, show_answers, created_by, created_at,
      identity_fields, panel_color, start_time, end_time, token_mode, exam_started_at)
    VALUES (exam_name, 'dummy_seed.pdf', 50000, tkn, tkn, questions_json, 'active',
      'medium', 0, 1, 1, guru_id, NOW() - interval '7 days' + guru_id * interval '1 day',
      identity_fields_json,
      CASE guru_id WHEN 2 THEN '#6366F1' WHEN 3 THEN '#10B981' WHEN 4 THEN '#F43F5E' WHEN 5 THEN '#F59E0B' WHEN 6 THEN '#06B6D4' END,
      NOW() - interval '6 days' + guru_id * interval '1 day',
      NOW() - interval '6 days' + guru_id * interval '1 day' + interval '2 hours',
      'static', NOW() - interval '7 days' + guru_id * interval '1 day')
    RETURNING id INTO exam_id;

    FOR j IN 7..9 LOOP
      INSERT INTO exam_pengawas (exam_id, user_id) VALUES (exam_id, j) ON CONFLICT DO NOTHING;
    END LOOP;

    FOR i IN 1..45 LOOP
      score := (40 + floor(random() * 61))::int;
      INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
        answers_json, score, start_time, created_at, identity_data)
      VALUES (exam_id, seed_rand_name(i + guru_id * 100),
        'SMP' || LPAD((i + guru_id * 100)::text, 4, '0'),
        class_name, seed_gen_answers(score), score,
        NOW() - interval '5 days' + guru_id * interval '1 day' + (random() * interval '1 hour'),
        NOW() - interval '5 days' + guru_id * interval '1 day' + (random() * interval '1 hour'),
        '{"student_name":"' || seed_rand_name(i + guru_id * 100) || '","exam_number":"SMP' || LPAD((i + guru_id * 100)::text, 4, '0') || '","student_class":"' || class_name || '"}');
    END LOOP;
  END LOOP;

  -- === SMA Negeri 2 Bandung (guru IDs 11-15) ===
  FOR guru_id IN 11..15 LOOP
    tkn := seed_rand_token();
    WHILE EXISTS (SELECT 1 FROM exams WHERE token = tkn OR active_token = tkn) LOOP
      tkn := seed_rand_token();
    END LOOP;

    exam_name := CASE guru_id
      WHEN 11 THEN 'Penilaian Akhir Semester Matematika Wajib Kelas 10'
      WHEN 12 THEN 'Penilaian Akhir Semester Fisika Kelas 11 IPA 1'
      WHEN 13 THEN 'Penilaian Akhir Semester Kimia Kelas 11 IPA 2'
      WHEN 14 THEN 'Penilaian Akhir Semester Ekonomi Kelas 12 IPS 1'
      WHEN 15 THEN 'Penilaian Akhir Semester Sosiologi Kelas 12 IPS 2'
    END;

    class_name := CASE guru_id
      WHEN 11 THEN '10' WHEN 12 THEN '11 IPA 1'
      WHEN 13 THEN '11 IPA 2' WHEN 14 THEN '12 IPS 1'
      WHEN 15 THEN '12 IPS 2'
    END;

    INSERT INTO exams (name, file_path, size_bytes, token, active_token, questions_json, status,
      security_level, strict_mode, public_results, show_answers, created_by, created_at,
      identity_fields, panel_color, start_time, end_time, token_mode, exam_started_at)
    VALUES (exam_name, 'dummy_seed.pdf', 75000, tkn, tkn, questions_json, 'active',
      'medium', 0, 1, 1, guru_id, NOW() - interval '5 days' + guru_id * interval '1 day',
      identity_fields_json,
      CASE guru_id WHEN 11 THEN '#8B5CF6' WHEN 12 THEN '#EC4899' WHEN 13 THEN '#14B8A6' WHEN 14 THEN '#F97316' WHEN 15 THEN '#6366F1' END,
      NOW() - interval '4 days' + guru_id * interval '1 day',
      NOW() - interval '4 days' + guru_id * interval '1 day' + interval '3 hours',
      'static', NOW() - interval '5 days' + guru_id * interval '1 day')
    RETURNING id INTO exam_id;

    FOR j IN 16..18 LOOP
      INSERT INTO exam_pengawas (exam_id, user_id) VALUES (exam_id, j) ON CONFLICT DO NOTHING;
    END LOOP;

    FOR i IN 1..42 LOOP
      score := (35 + floor(random() * 66))::int;
      INSERT INTO submissions (exam_id, student_name, exam_number, student_class,
        answers_json, score, start_time, created_at, identity_data)
      VALUES (exam_id, seed_rand_name(i + guru_id * 100),
        'SMA' || LPAD((i + guru_id * 100)::text, 4, '0'),
        class_name, seed_gen_answers(score), score,
        NOW() - interval '3 days' + guru_id * interval '1 day' + (random() * interval '2 hours'),
        NOW() - interval '3 days' + guru_id * interval '1 day' + (random() * interval '2 hours'),
        '{"student_name":"' || seed_rand_name(i + guru_id * 100) || '","exam_number":"SMA' || LPAD((i + guru_id * 100)::text, 4, '0') || '","student_class":"' || class_name || '"}');
    END LOOP;
  END LOOP;
END $$;

-- Drop helper functions
DROP FUNCTION seed_rand_token();
DROP FUNCTION seed_rand_name(INT);
DROP FUNCTION seed_gen_answers(INT);
