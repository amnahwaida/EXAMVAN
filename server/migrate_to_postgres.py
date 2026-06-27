"""
EXAMVAN SQLite → PostgreSQL Data Migration Script

Usage:
    python migrate_to_postgres.py

Requirements:
    - PostgreSQL must be running and accessible via DATABASE_URL env var
    - SQLite database must exist at the path specified by DATABASE_PATH env var
      (default: server/data/examvan.db)

What it does:
    1. Reads all data from SQLite
    2. Connects to PostgreSQL
    3. Applies schema.sql (idempotent)
    4. Migrates each table in dependency order
    5. Handles timestamp conversion (SQLite TEXT → TIMESTAMPTZ)
    6. Verifies row counts match
"""
import os
import sys
import json
from datetime import datetime, timezone

# Ensure we're in the server directory
os.chdir(os.path.dirname(os.path.abspath(__file__)))

from dotenv import load_dotenv

dotenv_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), '..', '.env')
if os.path.isfile(dotenv_path):
    load_dotenv(dotenv_path)

import sqlite3
import psycopg2
from psycopg2 import extras

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

SQLITE_PATH = os.environ.get('DATABASE_PATH', 'data/examvan.db')
if not os.path.isabs(SQLITE_PATH):
    SQLITE_PATH = os.path.join(os.path.dirname(os.path.abspath(__file__)), SQLITE_PATH)

PG_DSN = os.environ.get('DATABASE_URL', '')
if not PG_DSN:
    print("❌ DATABASE_URL environment variable not set!")
    sys.exit(1)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def to_timestamptz(val):
    """Convert SQLite timestamp (TEXT or None) to timezone-aware datetime."""
    if not val:
        return None
    try:
        if isinstance(val, str):
            # Handle ISO format or YYYY-MM-DD HH:MM:SS
            if 'T' in val:
                dt = datetime.fromisoformat(val.split('.')[0])
            else:
                dt = datetime.strptime(val.strip(), '%Y-%m-%d %H:%M:%S')
            if dt.tzinfo is None:
                dt = dt.replace(tzinfo=timezone.utc)
            return dt
        return val
    except Exception as e:
        print(f"  ⚠️  Failed to parse timestamp '{val}': {e}")
        return None


def migrate_table(cursor, sqlite_cursor, table, columns, order_by='id'):
    """Migrate a single table from SQLite to PostgreSQL.

    Args:
        cursor: psycopg2 cursor
        sqlite_cursor: sqlite3 cursor
        table: table name
        columns: list of column names (must match both DBs)
        order_by: ORDER BY clause for reading source data
    """
    col_list = ', '.join(columns)
    placeholders = ', '.join(['%s'] * len(columns))
    updates = ', '.join([f'{c} = EXCLUDED.{c}' for c in columns])

    # Read from SQLite
    sqlite_cursor.execute(f'SELECT {col_list} FROM {table} ORDER BY {order_by}')
    rows = sqlite_cursor.fetchall()
    if not rows:
        print(f"  ⏭️  {table}: 0 rows (skipped)")
        return 0

    # Convert rows (handle timestamps)
    ts_columns = {
        'created_at', 'start_time', 'end_time', 'updated_at',
        'applied_at', 'expires_at', 'otp_expiry',
    }
    col_set = set(columns)
    ts_cols = [i for i, c in enumerate(columns) if c in ts_columns or c.endswith('_at')]

    converted = []
    for row in rows:
        row = list(row)
        for idx in ts_cols:
            if idx < len(row):
                row[idx] = to_timestamptz(row[idx])
        converted.append(tuple(row))

    # Truncate target table
    cursor.execute(f'TRUNCATE TABLE {table} RESTART IDENTITY CASCADE')

    # Insert in batches
    batch_size = 500
    total = 0
    for i in range(0, len(converted), batch_size):
        batch = converted[i:i + batch_size]
        for row in batch:
            try:
                cursor.execute(
                    f'INSERT INTO {table} ({col_list}) VALUES ({placeholders})',
                    row
                )
                total += 1
            except Exception as e:
                print(f"  ⚠️  {table}: insert failed: {e}")
                print(f"     Row: {row}")
                raise

    print(f"  ✅ {table}: {total} rows migrated")
    return total


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    print("=" * 60)
    print("EXAMVAN SQLite → PostgreSQL Migration")
    print("=" * 60)

    # ---- Source: SQLite ----
    if not os.path.exists(SQLITE_PATH):
        print(f"❌ SQLite database not found at: {SQLITE_PATH}")
        sys.exit(1)

    sqlite_conn = sqlite3.connect(SQLITE_PATH)
    sqlite_conn.row_factory = sqlite3.Row
    sqlite_cursor = sqlite_conn.cursor()
    print(f"\n📦 Source: SQLite ({SQLITE_PATH})")
    print(f"   Size: {os.path.getsize(SQLITE_PATH) / 1024:.1f} KB")

    # ---- Target: PostgreSQL ----
    pg_conn = psycopg2.connect(PG_DSN)
    pg_conn.autocommit = False
    cursor = pg_conn.cursor()
    print(f"\n🎯 Target: PostgreSQL ({PG_DSN})")

    # ---- Apply schema ----
    schema_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'schema.sql')
    if os.path.exists(schema_path):
        with open(schema_path, 'r') as f:
            cursor.execute(f.read())
        pg_conn.commit()
        print("\n📋 Schema applied from schema.sql")
    else:
        print("\n⚠️  schema.sql not found — skipping schema creation")

    # ---- Migrate tables (dependency order) ----
    print("\n--- Migrating Tables ---")

    # 1. admin_users (no foreign key dependencies)
    sqlite_cursor.execute("PRAGMA table_info(admin_users)")
    cols = [r['name'] for r in sqlite_cursor.fetchall() if r['name'] != 'id']
    cols.insert(0, 'id')
    migrate_table(cursor, sqlite_cursor, 'admin_users', cols)

    # 2. saas_settings (no dependencies)
    migrate_table(cursor, sqlite_cursor, 'saas_settings', ['key', 'value'])

    # 3. rate_limits (no dependencies)
    sqlite_cursor.execute("PRAGMA table_info(rate_limits)")
    cols = [r['name'] for r in sqlite_cursor.fetchall() if r['name'] != 'id']
    cols.insert(0, 'id')
    migrate_table(cursor, sqlite_cursor, 'rate_limits', cols)

    # 4. exams (depends on admin_users via created_by FK)
    sqlite_cursor.execute("PRAGMA table_info(exams)")
    cols = [r['name'] for r in sqlite_cursor.fetchall() if r['name'] != 'id']
    cols.insert(0, 'id')
    migrate_table(cursor, sqlite_cursor, 'exams', cols)

    # 5. _migrations (no dependencies, but after main tables)
    sqlite_cursor.execute("PRAGMA table_info(_migrations)")
    cols = [r['name'] for r in sqlite_cursor.fetchall() if r['name'] != 'id']
    cols.insert(0, 'id')
    migrate_table(cursor, sqlite_cursor, '_migrations', cols)

    # 6. exam_pengawas (depends on exams, admin_users)
    sqlite_cursor.execute("PRAGMA table_info(exam_pengawas)")
    cols = [r['name'] for r in sqlite_cursor.fetchall() if r['name'] != 'id']
    cols.insert(0, 'id')
    migrate_table(cursor, sqlite_cursor, 'exam_pengawas', cols)

    # 7. submissions (depends on exams via exam_id FK)
    sqlite_cursor.execute("PRAGMA table_info(submissions)")
    cols = [r['name'] for r in sqlite_cursor.fetchall() if r['name'] != 'id']
    cols.insert(0, 'id')
    migrate_table(cursor, sqlite_cursor, 'submissions', cols)

    # 8. student_access_logs (depends on exams, submissions)
    sqlite_cursor.execute("PRAGMA table_info(student_access_logs)")
    cols = [r['name'] for r in sqlite_cursor.fetchall() if r['name'] != 'id']
    cols.insert(0, 'id')
    migrate_table(cursor, sqlite_cursor, 'student_access_logs', cols)

    # ---- Commit ----
    pg_conn.commit()
    print("\n✅ PostgreSQL migration committed successfully!")

    # ---- Verify ----
    print("\n--- Verification ---")
    tables = [
        'admin_users', 'saas_settings', 'rate_limits', 'exams',
        '_migrations', 'exam_pengawas', 'submissions', 'student_access_logs'
    ]
    for table in tables:
        sqlite_cursor.execute(f'SELECT COUNT(*) as cnt FROM {table}')
        pg_cursor = pg_conn.cursor()
        pg_cursor.execute(f'SELECT COUNT(*) as cnt FROM {table}')
        s_cnt = sqlite_cursor.fetchone()['cnt']
        p_cnt = pg_cursor.fetchone()[0]
        status = '✅' if s_cnt == p_cnt else '❌'
        print(f"  {status} {table}: SQLite={s_cnt} → PG={p_cnt}")

    # ---- Cleanup ----
    sqlite_conn.close()
    pg_conn.close()
    print("\n🎉 Migration complete!")


if __name__ == '__main__':
    main()
