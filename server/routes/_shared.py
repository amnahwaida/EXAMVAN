"""EXAMVAN routes — shared utilities across route modules."""
import json
import logging
from flask import jsonify

REQUIRED_ANDROID_VERSION = '2.2.0'

logger = logging.getLogger('examvan.routes')

# ===== Standardized Response Helpers =====

def error_response(message, code=400):
    """Return a standardized error JSON response."""
    return jsonify({'success': False, 'message': message}), code


def success_response(data=None, message=None):
    """Return a standardized success JSON response."""
    resp = {'success': True}
    if data is not None:
        resp['data'] = data
    if message:
        resp['message'] = message
    return jsonify(resp)


# ===== CSV / XLSX Sanitization =====

def _csv_safe(value):
    """Prevent CSV formula injection by prefixing dangerous chars with tab."""
    if isinstance(value, str) and value and value[0] in ('=', '+', '-', '@', '\t', '\r'):
        return '\t' + value
    return value


def _sanitize_xlsx(value):
    """Prevent Excel formula injection by prefixing dangerous characters with a single quote."""
    if isinstance(value, str) and value and value[0] in ('=', '+', '-', '@', '\t', '\r'):
        return "'" + value
    return value


# ===== Student Input Sanitization =====
import html

def sanitize_student_input(value):
    """Sanitize student text input: strip, escape HTML, limit length.
    Prevents XSS injection in admin panel display."""
    if not isinstance(value, str):
        return ''
    value = value.strip()
    value = value[:200]
    return html.escape(value, quote=True)


# ===== PDF Upload Validation =====

def validate_pdf_upload(file_data, filename, content_type, max_size=None):
    """Validate uploaded PDF file data. Returns (is_valid, error_message)."""
    from app import MAX_FILE_SIZE
    if max_size is None:
        max_size = MAX_FILE_SIZE

    allowed_pdf_types = ['application/pdf', 'application/x-pdf', 'application/octet-stream']
    if content_type not in allowed_pdf_types and not (filename and filename.lower().endswith('.pdf')):
        return False, 'Hanya file PDF yang diizinkan'

    if not file_data.startswith(b'%PDF'):
        return False, 'File tidak valid (bukan PDF)'

    if len(file_data) > max_size:
        size_mb = max_size // (1024 * 1024)
        return False, f'Ukuran file melebihi batas {size_mb}MB'

    return True, None


# ===== Account Expiry =====
from datetime import datetime, timezone

def days_until_expiry(expires_at_str):
    """Calculate days remaining until account expiry. Returns int or None."""
    if not expires_at_str:
        return None
    try:
        expires = datetime.strptime(expires_at_str, '%Y-%m-%d %H:%M:%S').replace(tzinfo=timezone.utc)
        remaining = (expires - datetime.now(timezone.utc)).days
        return max(remaining, 0)
    except Exception:
        return None


# ===== Identity Field Helpers =====

DEFAULT_IDENTITY_FIELDS = json.dumps([
    {'key': 'student_name', 'label': 'Nama', 'required': True},
    {'key': 'exam_number', 'label': 'Nomor Ujian', 'required': True},
    {'key': 'student_class', 'label': 'Kelas', 'required': True},
])


def _parse_identity_fields(exam_or_fields):
    """Parse identity_fields from exam row or direct list.

    exam_or_fields can be:
    - dict / sqlite3.Row (query result) → grab 'identity_fields' key
    - str JSON directly
    - list directly
    - None
    """
    if isinstance(exam_or_fields, list):
        return exam_or_fields

    try:
        raw = exam_or_fields['identity_fields'] or ''
    except (TypeError, KeyError, IndexError):
        raw = exam_or_fields or ''

    try:
        fields = json.loads(raw) if raw else []
        if fields and isinstance(fields, list):
            return fields
    except Exception:
        pass
    return json.loads(DEFAULT_IDENTITY_FIELDS)


def _get_identity_data(sub):
    """Parse identity_data from submission row, return dict."""
    try:
        raw = sub['identity_data'] if hasattr(sub, '__getitem__') else getattr(sub, 'identity_data', '')
        return json.loads(raw) if raw else {}
    except Exception:
        return {}


def _get_sub_field(sub, key, default=''):
    """Get field from submission row, handle sqlite3.Row or dict."""
    try:
        return sub[key]
    except (TypeError, KeyError, IndexError):
        try:
            return getattr(sub, key, default)
        except Exception:
            return default


def identity_headers_and_values(identity_fields, identity_data, sub):
    """Given identity_fields config and submission data, return (headers, values).

    headers — list of labels sesuai urutan identity_fields
    values  — list of values sesuai urutan identity_fields
    sub     — submission row (untuk fallback kolom lama)
    """
    headers = []
    values = []
    for f in identity_fields:
        key = f['key']
        label = f['label']
        headers.append(label)
        val = identity_data.get(key)
        if val is None or val == '':
            if key in ('student_name', 'exam_number', 'student_class'):
                val = _get_sub_field(sub, key, '')
            else:
                val = ''
        values.append(_sanitize_xlsx(str(val)) if val is not None else '—')
    return headers, values


# ===== Token Masking =====

def mask_token(token, visible_chars=4):
    """Mask a token showing only the last N characters."""
    if not token or len(token) <= visible_chars + 4:
        return token
    return '*' * (len(token) - visible_chars) + token[-visible_chars:]


# ===== Sheet Name Sanitization (XLSX) =====

def sanitize_sheet_name(name):
    """Sanitize sheet name for Excel (max 31 chars, no invalid chars)."""
    for ch in ['\\', '/', '?', '*', ':', '[', ']']:
        name = name.replace(ch, '')
    return name[:31] if name else 'Sheet'
