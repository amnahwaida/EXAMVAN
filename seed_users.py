"""
Seed script: Add test users to verify pagination on the manage users page.
Usage: python3 seed_users.py

⚠️⚠️⚠️ DEVELOPMENT ONLY — DO NOT RUN IN PRODUCTION ⚠️⚠️⚠️
This script creates users with default passwords for testing purposes.
All created users MUST have their passwords changed before production use.
"""
import sys
import os
import secrets
import sqlite3
from datetime import datetime, timezone, timedelta
from werkzeug.security import generate_password_hash

# Database path
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
DB_PATH = os.environ.get('DATABASE_PATH', os.path.join(BASE_DIR, 'server', 'data', 'examvan.db'))

# Fallback paths
if not os.path.exists(DB_PATH):
    for p in [
        os.path.join(BASE_DIR, 'data', 'examvan.db'),
        os.path.join(BASE_DIR, 'server', 'data', 'examvan.db'),
    ]:
        if os.path.exists(p):
            DB_PATH = p
            break

print(f"Using database: {DB_PATH}")

if not os.path.exists(DB_PATH):
    print(f"ERROR: Database not found at {DB_PATH}")
    sys.exit(1)

conn = sqlite3.connect(DB_PATH)
conn.row_factory = sqlite3.Row
db = conn.cursor()

# Check existing teacher count
existing = db.execute("SELECT COUNT(*) as cnt FROM admin_users WHERE username != 'admin'").fetchone()
print(f"Existing teacher users: {existing['cnt']}")

expires = (datetime.now(timezone.utc) + timedelta(days=365)).strftime('%Y-%m-%d %H:%M:%S')
now = datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M:%S')

teachers = [
    ("budi_santoso", "081111111001", 3, 1048576, 2, 1048576),
    ("siti_rahayu", "081111111002", 5, 2097152, 3, 2097152),
    ("agus_prasetyo", "081111111003", 3, 1048576, 2, 1048576),
    ("dewi_sartika", "081111111004", 5, 2097152, 3, 2097152),
    ("hendra_gunawan", "081111111005", 3, 1048576, 2, 1048576),
    ("rina_kusuma", "081111111006", 5, 2097152, 3, 2097152),
    ("bambang_susilo", "081111111007", 3, 1048576, 2, 1048576),
    ("maya_anggraeni", "081111111008", 5, 2097152, 3, 2097152),
    ("dwi_cahyo", "081111111009", 3, 1048576, 2, 1048576),
    ("fitri_handayani", "081111111010", 5, 2097152, 3, 2097152),
    ("eko_purnomo", "081111111011", 3, 1048576, 2, 1048576),
    ("tuti_maryati", "081111111012", 5, 2097152, 3, 2097152),
    ("adi_nugroho", "081111111013", 3, 1048576, 2, 1048576),
    ("dian_permatasari", "081111111014", 5, 2097152, 3, 2097152),
    ("agus_wibowo", "081111111015", 3, 1048576, 2, 1048576),
    ("sri_wahyuni", "081111111016", 5, 2097152, 3, 2097152),
    ("danang_prabowo", "081111111017", 3, 1048576, 2, 1048576),
    ("nurul_hidayah", "081111111018", 5, 2097152, 3, 2097152),
    ("tejo_saputro", "081111111019", 3, 1048576, 2, 1048576),
    ("rini_astuti", "081111111020", 5, 2097152, 3, 2097152),
    ("purnomo_sidiq", "081111111021", 3, 1048576, 2, 1048576),
    ("yuni_febriani", "081111111022", 5, 2097152, 3, 2097152),
    ("arif_hidayat", "081111111023", 3, 1048576, 2, 1048576),
    ("diah_ayuningtyas", "081111111024", 5, 2097152, 3, 2097152),
    ("slamet_riyadi", "081111111025", 3, 1048576, 2, 1048576),
]

created = 0
for username, wa, mx, pdf, draft, draft_size in teachers:
    existing_user = db.execute("SELECT id FROM admin_users WHERE username = ?", (username,)).fetchone()
    if existing_user:
        print(f"  SKIP {username} (already exists)")
        continue
    # Generate a unique random password per user
    pw_hash = generate_password_hash(secrets.token_hex(8))
    db.execute(
        "INSERT INTO admin_users (username, password_hash, whatsapp_number, status, max_exams, max_pdf_size, max_drafts, max_draft_size, expires_at, created_at, otp_code, otp_expiry) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)",
        (username, pw_hash, wa, 'active', mx, pdf, draft, draft_size, expires, now)
    )
    created += 1
    print(f"  CREATE {username} ({wa})")

conn.commit()
conn.close()
print(f"\nDone! Created {created} new teacher users.")
print(f"Total teacher users now: {existing['cnt'] + created}")
print("\n⚠️  Each user has a unique randomly generated password (printed above not available).")
print("    Users can reset their password via the admin change-password feature.")
