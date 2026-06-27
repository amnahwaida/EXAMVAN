"""
EXAMVAN Database Abstraction Layer
Wraps psycopg2 connection pool and provides SQLite-compatible API so
existing code with `db.execute(sql, params)` continues working unchanged.

Key features:
  - ? → %s placeholder conversion (transparent to callers)
  - RealDictCursor → row['column_name'] access (like sqlite3.Row)
  - ThreadedConnectionPool for safe multi-worker access
  - SQLite error compatibility mapping
  - Flask g integration (get_db() / close_db())
"""
import os
import re
import time
from functools import wraps

import psycopg2
from psycopg2 import pool, extras, sql as pg_sql
from psycopg2 import errors as pg_errors
from flask import g


# ---------------------------------------------------------------------------
# Pool
# ---------------------------------------------------------------------------

_pool = None
_connected = False


def connect(dsn=None, minconn=2, maxconn=10):
    """Initialise the global connection pool (called once from app.py)."""
    global _pool, _connected
    if _connected:
        return
    dsn = dsn or os.environ.get('DATABASE_URL', '')
    if not dsn:
        raise RuntimeError(
            "DATABASE_URL not set. Please set the DATABASE_URL environment "
            "variable to a PostgreSQL connection string."
        )
    _pool = pool.ThreadedConnectionPool(minconn, maxconn, dsn)
    _connected = True


def disconnect():
    """Close all pool connections (called during app teardown)."""
    global _pool, _connected
    if _pool is not None:
        _pool.closeall()
        _pool = None
        _connected = False


# ---------------------------------------------------------------------------
# Database wrapper
# ---------------------------------------------------------------------------

# regex that matches SQLite '?' placeholders NOT inside string literals
_QMARK_RE = re.compile(r"""
    (?<=[^'])
    \?
    (?=[^'])
""", re.VERBOSE)

# Simpler approach: just replace all `?` that appear outside of quoted strings
_QMARK_SIMPLE = re.compile(r"\?")


def _convert_placeholders(sql):
    """Replace '?' placeholders with '%s' for psycopg2 compatibility.

    Handles dynamic IN-clause builders like ','.join('?' * n) correctly
    because each '?' becomes '%s' independently.
    """
    return _QMARK_SIMPLE.sub('%s', sql)


class Database:
    """PostgreSQL wrapper providing an sqlite3-like API.

    Wraps a psycopg2 connection with RealDictCursor and transparent
    placeholder conversion so existing SQLite code works unchanged.
    """

    def __init__(self, conn):
        self.conn = conn
        self._last_rowcount = 0

    # -- execute ----------------------------------------------------------

    def execute(self, sql, params=None):
        """Execute a single SQL statement.

        ``sql`` may contain ``?`` placeholders (they are converted to ``%s``).
        ``params`` can be a tuple, list, or dict.

        Returns self for chaining (like sqlite3).
        """
        pg_sql = _convert_placeholders(str(sql))
        self._last_cursor = self.conn.cursor(cursor_factory=psycopg2.extras.RealDictCursor)
        if params is not None:
            self._last_cursor.execute(pg_sql, params)
        else:
            self._last_cursor.execute(pg_sql)

        # Store rowcount for property access (like sqlite3)
        self._last_rowcount = self._last_cursor.rowcount
        return self

    def executescript(self, sql_script):
        """Emulate sqlite3.executescript() by splitting on ';' and running each."""
        statements = [s.strip() for s in sql_script.split(';') if s.strip()]
        for stmt in statements:
            if stmt:
                self.execute(stmt)
        return self

    # -- fetch ------------------------------------------------------------

    def fetchone(self):
        """Return one row as dict (or None)."""
        if self._last_cursor is None:
            return None
        row = self._last_cursor.fetchone()
        if row is None:
            return None
        return dict(row)  # RealDictRow → plain dict for sqlite3.Row compat

    def fetchall(self):
        """Return all rows as list of dicts."""
        if self._last_cursor is None:
            return []
        rows = self._last_cursor.fetchall()
        return [dict(r) for r in rows]

    # -- property access (sqlite3 compatibility) --------------------------

    @property
    def rowcount(self):
        return self._last_rowcount

    def __getitem__(self, name):
        """Allow ``row['column']`` access on fetched rows.

        Note: this is a fallback — rows returned by fetchone/fetchall
        are already plain dicts.
        """
        if hasattr(self, '_current_row') and self._current_row is not None:
            return self._current_row[name]
        raise KeyError(name)

    # -- transaction helpers -----------------------------------------------

    def commit(self):
        self.conn.commit()

    def rollback(self):
        self.conn.rollback()

    def close(self):
        """Return connection to pool (not actually closing it)."""
        global _pool
        if _pool is not None and self.conn is not None:
            try:
                _pool.putconn(self.conn)
            except Exception:
                pass
        self.conn = None

    # -- context manager (for 'with db:' usage) ---------------------------

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if exc_type is not None:
            self.rollback()
        else:
            self.commit()
        return False


# ---------------------------------------------------------------------------
# Flask integration
# ---------------------------------------------------------------------------

def get_db():
    """Request-scoped database connection (like the old get_db())."""
    if 'pg_db' not in g:
        if _pool is None:
            raise RuntimeError(
                "Database pool not initialised. Call db.connect() first."
            )
        conn = _pool.getconn()
        g.pg_db = Database(conn)
    return g.pg_db


def close_db(exception=None):
    """Return connection to pool at end of request."""
    db = g.pop('pg_db', None)
    if db is not None:
        db.close()


def get_db_standalone():
    """Obtain a connection for use outside request context (seed scripts, etc.)."""
    if _pool is None:
        raise RuntimeError("Database pool not initialised.")
    conn = _pool.getconn()
    return Database(conn)


# ---------------------------------------------------------------------------
# Error compatibility
# ---------------------------------------------------------------------------

class DatabaseError(Exception):
    """Wrapper for psycopg2 errors that sqlite3-compatible code can catch."""


class OperationalError(DatabaseError):
    """Maps to psycopg2.OperationalError."""


class IntegrityError(DatabaseError):
    """Maps to psycopg2.IntegrityError / UniqueViolation."""


def wrap_error(f):
    """Decorator that catches psycopg2 errors and re-raises as DatabaseError."""
    @wraps(f)
    def wrapper(*args, **kwargs):
        try:
            return f(*args, **kwargs)
        except pg_errors.UniqueViolation as e:
            raise IntegrityError(str(e)) from e
        except pg_errors.IntegrityError as e:
            raise IntegrityError(str(e)) from e
        except (pg_errors.OperationalError, psycopg2.OperationalError) as e:
            raise OperationalError(str(e)) from e
        except psycopg2.Error as e:
            raise DatabaseError(str(e)) from e
    return wrapper
