"""
EXAMVAN Helpers — pure utility functions (no Flask dependency).
"""
import json
import logging
from datetime import datetime, timezone, timedelta
import socket

logger = logging.getLogger('examvan.helpers')


def localize_date_string(utc_str, tz_offset_min=None):
    """Localize database UTC string using a browser timezone offset in minutes."""
    if not utc_str:
        return '—'
    try:
        iso_str = utc_str.strip()
        if ' ' in iso_str:
            iso_str = iso_str.replace(' ', 'T')
        if not iso_str.endswith('Z'):
            iso_str += 'Z'
        dt = datetime.fromisoformat(iso_str.replace('Z', '+00:00'))
        if tz_offset_min is not None:
            local_dt = dt - timedelta(minutes=tz_offset_min)
            return local_dt.strftime('%Y-%m-%d %H:%M:%S')
        else:
            return dt.strftime('%Y-%m-%d %H:%M:%S UTC')
    except Exception as e:
        logger.error("Localization error: %s", e)
        return utc_str


def format_iso_utc(date_str):
    """Convert SQLite YYYY-MM-DD HH:MM:SS string to ISO 8601 UTC format."""
    if not date_str:
        return date_str
    if ' ' in date_str:
        return date_str.replace(' ', 'T') + 'Z'
    if 'T' in date_str or date_str.endswith('Z'):
        return date_str
    return date_str + 'Z'


def get_local_ip():
    """Get the server's LAN IP address, offline-friendly."""
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.settimeout(1)
        s.connect(('10.255.255.255', 1))
        ip = s.getsockname()[0]
        s.close()
        if ip and ip != '127.0.0.1' and not ip.startswith('127.'):
            return ip
    except Exception:
        pass
    try:
        hostname = socket.gethostname()
        ips = socket.gethostbyname_ex(hostname)[2]
        for ip in ips:
            if not ip.startswith('127.') and not ip.startswith('172.'):
                return ip
    except Exception:
        pass
    return '127.0.0.1'


# ===== Scoring Engine =====

def _normalize_q_num(number):
    """Normalize question number: handle float representation (e.g. 1.0 -> '1')."""
    try:
        num_val = float(number)
        return str(int(num_val)) if num_val.is_integer() else str(number)
    except Exception:
        return str(number)


def _evaluate_single_question(student_ans, correct_ans, q_type, q_weight, partial_scoring):
    """Evaluate a single question. Returns (earned, status_text, status_class)."""
    if student_ans is None or correct_ans is None:
        if student_ans is None:
            return 0.0, 'unanswered', 'unanswered'
        return 0.0, 'incorrect', 'incorrect'

    if q_type in ('single_choice', 'true_false', 'short_answer'):
        s_norm = ' '.join(str(student_ans).split()).upper()
        c_norm = ' '.join(str(correct_ans).split()).upper()
        if s_norm == c_norm:
            return q_weight, 'correct', 'correct'
        return 0.0, 'incorrect', 'incorrect'

    if q_type == 'multiple_choice':
        if isinstance(student_ans, list) and isinstance(correct_ans, list):
            if partial_scoring:
                correct_set = set(str(x).upper() for x in correct_ans)
                student_set = set(str(x).upper() for x in student_ans)
                if correct_set:
                    correct_selected = sum(1 for x in student_set if x in correct_set)
                    incorrect_selected = sum(1 for x in student_set if x not in correct_set)
                    portion = max(0.0, (correct_selected - incorrect_selected) / len(correct_set))
                    earned = portion * q_weight
                    if portion >= 1.0:
                        return earned, 'correct', 'correct'
                    elif portion > 0:
                        return earned, 'partial', 'partial'
                    return 0.0, 'incorrect', 'incorrect'
            else:
                if sorted(str(x).upper() for x in student_ans) == sorted(str(x).upper() for x in correct_ans):
                    return q_weight, 'correct', 'correct'
        return 0.0, 'incorrect', 'incorrect'

    if q_type == 'matching':
        if isinstance(student_ans, dict) and isinstance(correct_ans, dict):
            if partial_scoring:
                if correct_ans:
                    correct_matches = sum(
                        1 for k, v in correct_ans.items()
                        if str(student_ans.get(k, '')).strip().upper() == str(v).strip().upper()
                    )
                    portion = correct_matches / len(correct_ans)
                    earned = portion * q_weight
                    if portion >= 1.0:
                        return earned, 'correct', 'correct'
                    elif portion > 0:
                        return earned, 'partial', 'partial'
                    return 0.0, 'incorrect', 'incorrect'
            else:
                match = all(
                    str(student_ans.get(k, '')).strip().upper() == str(v).strip().upper()
                    for k, v in correct_ans.items()
                )
                if match:
                    return q_weight, 'correct', 'correct'
        return 0.0, 'incorrect', 'incorrect'

    return 0.0, 'incorrect', 'incorrect'


def evaluate_answers_detailed(answers, questions):
    """Evaluate all answers against questions. Returns dict keyed by q_num."""
    evaluation = {}
    if not questions:
        return evaluation
    for q in questions:
        q_num = _normalize_q_num(q.get('number', q.get('key', '')))
        student_ans = answers.get(q_num)
        # Some questions store answer in 'answer' field, others in 'key'
        correct_ans = q.get('answer', q.get('key'))
        q_weight = float(q.get('weight', q.get('score', 1.0)))
        partial_scoring = q.get('partial_scoring', False)
        earned, status_text, status_class = _evaluate_single_question(
            student_ans, correct_ans, q.get('type', 'single_choice'), q_weight, partial_scoring)
        evaluation[q_num] = {
            'earned': earned,
            'statusText': status_text,
            'statusClass': status_class,
        }
    return evaluation


def calculate_submission_score(answers, questions):
    """Calculate total score for a student submission."""
    if not questions:
        return None
    try:
        evaluation = evaluate_answers_detailed(answers, questions)
        total = sum(e['earned'] for e in evaluation.values())
        return round(total, 2)
    except Exception as e:
        logger.error("Scoring calculation error: %s", e)
        return None


# ===== Role Helpers (multi-role JSON array support) =====

def parse_roles(role_str):
    """Parse role column into a list of roles. Handles legacy single-role and JSON array formats."""
    if not role_str:
        return ['guru']
    if role_str == 'superadmin':
        return ['superadmin']
    try:
        roles = json.loads(role_str)
        if isinstance(roles, list):
            return roles
    except (json.JSONDecodeError, TypeError):
        pass
    return [role_str]


def has_role(role_str, target_role):
    """Check if a role string includes the target role."""
    return target_role in parse_roles(role_str)


def serialize_roles(roles):
    """Serialize a list of roles to JSON string for storage."""
    return json.dumps(roles)


def display_roles(role_str):
    """Return human-readable role labels from a role string."""
    role_map = {'superadmin': 'Super Admin', 'guru': 'Guru', 'pengawas': 'Pengawas'}
    roles = parse_roles(role_str)
    return ', '.join(role_map.get(r, r) for r in roles)
