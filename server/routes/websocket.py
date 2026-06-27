"""
EXAMVAN WebSocket event handlers (Flask-SocketIO).

Handles real-time communication between:
  - Android app (student heartbeat, status updates)
  - Admin browser (pengawas monitoring, live updates)
"""
import json
from datetime import datetime, timezone

from flask import request, session

from app import (
    app, socketio, get_db, redis_client,
    admin_required, set_student_heartbeat,
)


# ===== Admin / Pengawas Events =====

@socketio.on('connect', namespace='/admin')
def admin_connect():
    """Admin connects to WebSocket. Verify session."""
    if 'admin_id' not in session:
        return False
    print(f"[WS] Admin connected: {session.get('admin_username', 'unknown')}")
    return True


@socketio.on('disconnect', namespace='/admin')
def admin_disconnect():
    print(f"[WS] Admin disconnected")


@socketio.on('join_exam', namespace='/admin')
def admin_join_exam(data):
    """Admin/pengawas joins an exam monitoring room."""
    exam_id = data.get('exam_id')
    if not exam_id:
        return
    # Verify this user can monitor this exam
    db = get_db()
    user_id = session.get('admin_id')
    is_privileged = session.get('is_super_admin') or session.get('is_operator')
    if not is_privileged:
        assignment = db.execute(
            'SELECT id FROM exam_pengawas WHERE exam_id = ? AND user_id = ?',
            (exam_id, user_id)
        ).fetchone()
        if not assignment:
            return
    room = f'exam_{exam_id}'
    join_room(room)
    print(f"[WS] Admin joined room: {room}")


@socketio.on('leave_exam', namespace='/admin')
def admin_leave_exam(data):
    """Admin leaves an exam monitoring room."""
    exam_id = data.get('exam_id')
    if exam_id:
        leave_room(f'exam_{exam_id}')


# ===== Android / Student Events =====

@socketio.on('connect', namespace='/student')
def student_connect():
    """Student device connects. Verify via token."""
    auth = request.args.to_dict()
    token = auth.get('token', '')
    if not token:
        return False
    # Validate token
    db = get_db()
    exam = db.execute(
        'SELECT id, status FROM exams WHERE token = ?',
        (token,)
    ).fetchone()
    if not exam or exam['status'] != 'active':
        return False
    # Store exam_id in the session
    session['ws_exam_id'] = exam['id']
    join_room(f'exam_{exam["id"]}')
    return True


@socketio.on('heartbeat', namespace='/student')
def student_heartbeat(data):
    """Receive heartbeat from Android app. Write to Redis."""
    exam_id = session.get('ws_exam_id') or data.get('exam_id')
    mac_address = data.get('mac_address', '')
    if not exam_id or not mac_address:
        return

    hb_data = {
        'student_name': data.get('student_name', ''),
        'exam_number': data.get('exam_number', ''),
        'student_class': data.get('student_class', ''),
        'device_info': data.get('device_info', ''),
        'ip_address': request.remote_addr or '',
        'event': 'heartbeat',
        'last_seen': datetime.now(timezone.utc).isoformat(),
    }
    set_student_heartbeat(exam_id, mac_address, hb_data)

    # Broadcast to admin room
    if socketio:
        socketio.emit('student_update', {
            'exam_id': exam_id,
            'mac_address': mac_address,
            'event': 'heartbeat',
            'timestamp': hb_data['last_seen'],
        }, room=f'exam_{exam_id}', namespace='/admin')


@socketio.on('exam_completed', namespace='/student')
def student_completed(data):
    """Student has submitted their answers."""
    exam_id = session.get('ws_exam_id') or data.get('exam_id')
    mac_address = data.get('mac_address', '')
    if not exam_id or not mac_address:
        return

    # Clear heartbeat from Redis (student is done)
    if redis_client:
        try:
            redis_client.delete(f'hb:{exam_id}:{mac_address}')
        except Exception:
            pass

    if socketio:
        socketio.emit('student_update', {
            'exam_id': exam_id,
            'mac_address': mac_address,
            'event': 'completed',
            'timestamp': datetime.now(timezone.utc).isoformat(),
        }, room=f'exam_{exam_id}', namespace='/admin')
