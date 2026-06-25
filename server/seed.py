"""Seed script: 2 instansi, real-case exam data."""
import sys, os, json, random, string, io
from datetime import datetime, timedelta, timezone
from werkzeug.security import generate_password_hash

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from app import app, get_db_standalone, init_db, BASE_DIR, STORAGE_DIR
from helpers import serialize_roles

random.seed(42)

INSTANSI = [
    {"name": "SMA Negeri 1 Jakarta",   "slug": "sma1-jkt"},
    {"name": "SMA Negeri 2 Bandung",   "slug": "sma2-bdg"},
]

USERS_PER_INSTANSI = [
    # (username, password, roles, nama)
    ("guru_jkt1", "guru123", ["guru"],             "Budi Santoso"),
    ("guru_jkt2", "guru123", ["guru"],             "Siti Rahmawati"),
    ("guru_jkt3", "guru123", ["guru", "pengawas"], "Ahmad Hidayat"),
    ("guru_bdg1", "guru123", ["guru"],             "Dewi Lestari"),
    ("guru_bdg2", "guru123", ["guru"],             "Rudi Hermawan"),
    ("guru_bdg3", "guru123", ["guru", "pengawas"], "Fitriani Nurul"),
]

EXAMS_PER_GURU = 2

SUBJECTS = [
    "Matematika Wajib", "Fisika", "Kimia", "Biologi",
    "Bahasa Indonesia", "Bahasa Inggris", "Sejarah", "Geografi",
    "Ekonomi", "Sosiologi", "Matematika Peminatan", "PKN",
]

KELAS = ["X-A", "X-B", "X-C", "XI-A", "XI-B", "XI-C", "XII-A", "XII-B", "XII-C"]

NAMA_DEPAN = [
    "Adi", "Bima", "Citra", "Dian", "Eka", "Farhan", "Gita", "Hendra",
    "Indah", "Joko", "Kiki", "Lina", "Mega", "Nanda", "Oscar", "Putri",
    "Qori", "Rama", "Sari", "Teguh", "Umi", "Vina", "Wawan", "Xena",
    "Yoga", "Zahra", "Agung", "Bella", "Cahyo", "Dina", "Edi", "Fani",
    "Galih", "Hani", "Irfan", "Juna", "Kartika", "Luki", "Mira", "Niko",
    "Olivia", "Pram", "Ratna", "Sandi", "Tari", "Usman", "Vita", "Winda",
]

NAMA_BELAKANG = [
    "Pratama", "Wijaya", "Kusuma", "Nugraha", "Utami", "Handayani", "Saputra",
    "Wulandari", "Hidayat", "Setiawan", "Anggraini", "Purnama", "Susanti",
]

def buat_mac():
    return ":".join(f"{random.randint(0,255):02x}" for _ in range(6))

def buat_siswa():
    dp = random.choice(NAMA_DEPAN)
    bl = random.choice(NAMA_BELAKANG)
    return f"{dp} {bl}"

def buat_nama_ujian(guru_nama, mapel, ke):
    return f"UH {mapel} - {guru_nama.split()[0]} #{ke}"

def seed():
    db = get_db_standalone()

    user_ids = []

    for idx, instansi in enumerate(INSTANSI):
        ins_name = instansi["name"]
        print(f"\n=== {ins_name} ===")

        # Users for this instansi (2 guru + 1 guru+pengawas)
        for ui in range(3):
            raw = USERS_PER_INSTANSI[idx * 3 + ui]
            username, password, roles, fullname = raw
            pw_hash = generate_password_hash(password)
            db.execute(
                'INSERT INTO admin_users (username, password_hash, role, instansi, status) VALUES (?, ?, ?, ?, ?)',
                (username, pw_hash, serialize_roles(roles), ins_name, 'active')
            )
            db.commit()
            uid = db.execute('SELECT id FROM admin_users WHERE username = ?', (username,)).fetchone()['id']
            user_ids.append((uid, username, roles, fullname))
            print(f"  User: {username} ({', '.join(roles)}) — {fullname}")

    print(f"\n=== MEMBUAT UJIAN ===")

    exam_ids = []

    # Each guru creates exams
    for uid, username, roles, fullname in user_ids:
        if "guru" not in roles:
            continue
        instansi = db.execute('SELECT instansi FROM admin_users WHERE id = ?', (uid,)).fetchone()['instansi']
        for ke in range(1, EXAMS_PER_GURU + 1):
            mapel = random.choice(SUBJECTS)
            nama = buat_nama_ujian(fullname, mapel, ke)
            token = ''.join(random.choices(string.ascii_uppercase + string.digits, k=6))

            # Create a dummy PDF
            safe_name = f"{token}.pdf"
            fpath = os.path.join(STORAGE_DIR, safe_name)
            os.makedirs(STORAGE_DIR, exist_ok=True)
            with open(fpath, 'wb') as f:
                f.write(b'%PDF-1.4\nDummy exam content\n')

            size = random.randint(50000, 500000)
            db.execute(
                'INSERT INTO exams (name, file_path, size_bytes, token, status, created_by, security_level, strict_mode, public_results, show_answers, questions_json, identity_fields, panel_color, start_time, end_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
                (nama, safe_name, size, token, 'active', uid, random.choice(['low', 'medium', 'high']), random.choice([0, 1]), 1, random.choice([0, 1]),
                 json.dumps([
                     {"key": "soal_1", "label": "Soal 1", "type": "multiple_choice", "options": ["A", "B", "C", "D", "E"], "score": 4, "answer": random.choice(["A", "B", "C", "D", "E"])},
                     {"key": "soal_2", "label": "Soal 2", "type": "multiple_choice", "options": ["A", "B", "C", "D", "E"], "score": 4, "answer": random.choice(["A", "B", "C", "D", "E"])},
                     {"key": "soal_3", "label": "Soal 3", "type": "multiple_choice", "options": ["A", "B", "C", "D", "E"], "score": 4, "answer": random.choice(["A", "B", "C", "D", "E"])},
                     {"key": "soal_4", "label": "Soal 4", "type": "multiple_choice", "options": ["A", "B", "C", "D", "E"], "score": 4, "answer": random.choice(["A", "B", "C", "D", "E"])},
                     {"key": "soal_5", "label": "Soal 5", "type": "multiple_choice", "options": ["A", "B", "C", "D", "E"], "score": 4, "answer": random.choice(["A", "B", "C", "D", "E"])},
                 ]),
                 json.dumps([
                     {"key": "student_name", "label": "Nama", "required": True},
                     {"key": "exam_number", "label": "Nomor Ujian", "required": True},
                     {"key": "student_class", "label": "Kelas", "required": True},
                     {"key": "nisn", "label": "NISN", "required": False},
                 ]),
                 random.choice(["#6366F1", "#10B981", "#F59E0B", "#EF4444", "#8B5CF6"]),
                 None, None)
            )
            db.commit()
            eid = db.execute('SELECT id FROM exams WHERE token = ?', (token,)).fetchone()['id']
            exam_ids.append((eid, uid, username, instansi, nama, token))
            print(f"  [{instansi}] {nama} (token: {token}) — by {username}")

    print(f"\n=== ASSIGN PENGAWAS ===")

    # Assign pengawas: guru+pengawas di instansi sama mengawasi ujian guru lain di instansi yg sama
    for eid, uid, username, instansi, enama, token in exam_ids:
        creator_instansi = instansi
        # Find pengawas in same instansi (excluding the creator)
        pengawas_candidates = db.execute(
            'SELECT id, username FROM admin_users WHERE instansi = ? AND role LIKE ? AND id != ?',
            (creator_instansi, '%"pengawas"%', uid)
        ).fetchall()

        # Assign first pengawas candidate if exists
        if pengawas_candidates:
            pw = random.choice(pengawas_candidates)
            try:
                db.execute('INSERT INTO exam_pengawas (exam_id, user_id) VALUES (?, ?)', (eid, pw['id']))
                db.commit()
                print(f"  {enama} → pengawas: {pw['username']}")
            except Exception:
                pass

    print(f"\n=== MEMBUAT SUBMISSIONS + ACCESS LOGS ===")

    for eid, uid, username, instansi, enama, token in exam_ids:
        # ~40 students per exam
        jml_siswa = random.randint(38, 44)
        print(f"  {enama}: {jml_siswa} siswa")

        for si in range(jml_siswa):
            s_nama = buat_siswa()
            s_no = f"{random.randint(2024001, 2024999)}"
            s_kelas = random.choice(KELAS)
            s_nisn = f"{random.randint(1000000000, 9999999999)}"
            s_mac = buat_mac()

            answers = {}
            total_score = 0
            for qi in range(1, 6):
                jawab = random.choice(["A", "B", "C", "D", "E"])
                answers[f"soal_{qi}"] = jawab
                if random.random() < 0.7:
                    total_score += 4

            id_data = json.dumps({
                "student_name": s_nama, "exam_number": s_no,
                "student_class": s_kelas, "nisn": s_nisn,
            })

            # Simulate some students not submitting (dropout ~5%)
            if random.random() < 0.05:
                # Started but didn't submit
                start = datetime.now(timezone.utc) - timedelta(hours=random.randint(1, 48))
                db.execute(
                    'INSERT INTO submissions (exam_id, student_name, exam_number, student_class, identity_data, answers_json, score, start_time, mac_address) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
                    (eid, s_nama, s_no, s_kelas, id_data, '', None, start.strftime('%Y-%m-%d %H:%M:%S'), s_mac)
                )
            else:
                # Full submission
                start = datetime.now(timezone.utc) - timedelta(hours=random.randint(1, 48))
                submit = start + timedelta(minutes=random.randint(15, 120))
                db.execute(
                    'INSERT INTO submissions (exam_id, student_name, exam_number, student_class, identity_data, answers_json, score, start_time, mac_address) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
                    (eid, s_nama, s_no, s_kelas, id_data, json.dumps(answers), total_score, start.strftime('%Y-%m-%d %H:%M:%S'), s_mac)
                )
            db.commit()
            sub_id = db.execute('SELECT id FROM submissions WHERE exam_id = ? AND mac_address = ? ORDER BY id DESC LIMIT 1', (eid, s_mac)).fetchone()['id']

            # Access logs: some students login multiple times (keluar-masuk)
            n_login = random.choices([1, 2, 3, 4], weights=[40, 30, 20, 10])[0]
            t = start - timedelta(minutes=random.randint(5, 30))
            for li in range(n_login):
                t += timedelta(minutes=random.randint(1, 15))
                db.execute(
                    'INSERT INTO student_access_logs (exam_id, submission_id, student_identifier, student_name, exam_number, student_class, event, ip_address, device_info, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
                    (eid, sub_id, s_mac, s_nama, s_no, s_kelas, 'login', f'192.168.{random.randint(0,255)}.{random.randint(1,254)}', f'Android {random.choice(["11", "12", "13", "14"])}', t.strftime('%Y-%m-%d %H:%M:%S'))
                )
                # Sometimes logout
                if random.random() < 0.6:
                    t += timedelta(minutes=random.randint(20, 90))
                    db.execute(
                        'INSERT INTO student_access_logs (exam_id, submission_id, student_identifier, student_name, exam_number, student_class, event, ip_address, device_info, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
                        (eid, sub_id, s_mac, s_nama, s_no, s_kelas, 'logout', f'192.168.{random.randint(0,255)}.{random.randint(1,254)}', f'Android {random.choice(["11", "12", "13", "14"])}', t.strftime('%Y-%m-%d %H:%M:%S'))
                    )
                # Heartbeat
                for _ in range(random.randint(0, 3)):
                    t += timedelta(minutes=random.randint(3, 10))
                    db.execute(
                        'INSERT INTO student_access_logs (exam_id, submission_id, student_identifier, student_name, exam_number, student_class, event, ip_address, device_info, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
                        (eid, sub_id, s_mac, s_nama, s_no, s_kelas, 'heartbeat', f'192.168.{random.randint(0,255)}.{random.randint(1,254)}', f'Android {random.choice(["11", "12", "13", "14"])}', t.strftime('%Y-%m-%d %H:%M:%S'))
                    )
                db.commit()

    print(f"\n=== SEED COMPLETE ===")


if __name__ == '__main__':
    init_db()
    seed()
