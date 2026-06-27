"""
EXAMVAN Async Submission Queue — Redis-backed.

Flow:
  1. POST /api/exams/<id>/submit → api_submit_exam()
     → validate → enqueue job to Redis List → return 202 {status: "queued"}
  2. Background worker thread:
     → BRPOP from Redis queue (blocking wait)
     → Read questions_json from PostgreSQL
     → Score via calculate_submission_score()
     → INSERT INTO submissions
     → Broadcast via SocketIO to pengawas room
     → Store result in Redis for polling

Why async?
  - Submit endpoint returns instantly (~5ms instead of ~50ms+ DB write)
  - DB writes are serialized to avoid contention under high load
  - Scoring happens off-request-path (non-blocking)
  - At 100.000 concurrent submits, queue absorbs the spike

Graceful degradation:
  - If Redis is unavailable, submissions fall back to synchronous (old behavior)
  - If worker is down, submissions queue up and process when worker starts
"""
import json
import time
import threading
import logging
import os
from datetime import datetime, timezone

logger = logging.getLogger('examvan')

# Queue key in Redis
SUBMISSION_QUEUE_KEY = 'examvan:submissions:pending'
SUBMISSION_RESULT_PREFIX = 'examvan:submission:result:'

# Queue stats
queue_stats_key = 'examvan:submissions:stats'

_job_id_counter = 0


def enqueue_submission(redis_client, job_data):
    """
    Push a submission job to the Redis queue.

    Args:
        redis_client: Redis connection
        job_data: dict with keys: exam_id, student_name, exam_number,
                  student_class, identity_data, answers_json, start_time,
                  mac_address

    Returns:
        str: job_id for polling result
    """
    global _job_id_counter
    _job_id_counter += 1
    job_id = f"{int(time.time())}-{_job_id_counter}"

    job = {
        'job_id': job_id,
        'received_at': datetime.now(timezone.utc).isoformat(),
        **job_data
    }

    # Push to Redis list (FIFO)
    redis_client.lpush(SUBMISSION_QUEUE_KEY, json.dumps(job))

    # Increment stats
    redis_client.hincrby(queue_stats_key, 'enqueued', 1)

    logger.info(f"Submission enqueued: job={job_id} exam={job_data.get('exam_id')} student={job_data.get('student_name', '')[:20]}")
    return job_id


def get_job_result(redis_client, job_id, timeout=30):
    """
    Poll for a submission result (for clients that want confirmation).

    Args:
        redis_client: Redis connection
        job_id: The job_id returned by enqueue_submission()
        timeout: Max seconds to wait (default 30s for normal, 0 for instant check)

    Returns:
        dict or None: {status, score, submission_id} or None if not yet processed
    """
    key = f"{SUBMISSION_RESULT_PREFIX}{job_id}"
    result = redis_client.get(key)
    if result:
        return json.loads(result)
    return None


def wait_for_result(redis_client, job_id, timeout=30):
    """
    Block until a submission result is available or timeout.
    Uses Redis BLPOP on a per-job result channel.
    """
    result = get_job_result(redis_result_key=job_id, redis_client=redis_client)
    if result:
        return result

    # Poll with exponential backoff
    for wait in [0.1, 0.2, 0.5, 1.0, 2.0]:
        time.sleep(wait)
        result = get_job_result(redis_client, job_id)
        if result:
            return result

    return None


def store_job_result(redis_client, job_id, result_data, ttl=300):
    """
    Store a completed submission result in Redis (5 min TTL).

    Args:
        redis_client: Redis connection
        job_id: The job_id from enqueue_submission()
        result_data: dict with {status, score, submission_id}
        ttl: Time-to-live in seconds
    """
    key = f"{SUBMISSION_RESULT_PREFIX}{job_id}"
    redis_client.setex(key, ttl, json.dumps(result_data))


def process_submissions(redis_client, get_db_func):
    """
    Background worker thread: continuously processes submissions from Redis queue.

    Args:
        redis_client: Redis connection
        get_db_func: callable that returns a Database wrapper (standalone, not request-scoped)
    """
    from helpers import calculate_submission_score

    logger.info("[Worker] Submission processor started")

    while True:
        try:
            # BRPOP: blocking pop from queue (waits up to 5 seconds)
            result = redis_client.brpop(SUBMISSION_QUEUE_KEY, timeout=5)

            if result is None:
                # No job available, loop again
                continue

            _, job_json = result
            job = json.loads(job_json)
            job_id = job.get('job_id', 'unknown')
            exam_id = job.get('exam_id')

            logger.info(f"[Worker] Processing job {job_id} for exam {exam_id}")

            # Process the submission
            db = get_db_func()
            try:
                _process_single_submission(db, job, redis_client)
                redis_client.hincrby(queue_stats_key, 'processed', 1)
            except Exception as e:
                logger.error(f"[Worker] Failed to process job {job_id}: {e}", exc_info=True)
                store_job_result(redis_client, job_id, {
                    'status': 'error',
                    'message': str(e),
                    'score': None,
                })
                redis_client.hincrby(queue_stats_key, 'errors', 1)
            finally:
                db.close()

        except KeyboardInterrupt:
            logger.info("[Worker] Submission processor stopped")
            break
        except Exception as e:
            logger.error(f"[Worker] Queue error: {e}")
            time.sleep(5)  # Back off on persistent errors


def _process_single_submission(db, job, redis_client):
    """
    Process a single submission job:
    1. Read exam questions
    2. Score the answers
    3. Insert into submissions table
    4. Broadcast via SocketIO
    """
    from helpers import calculate_submission_score

    job_id = job['job_id']
    exam_id = job['exam_id']

    # 1. Fetch exam questions for scoring
    exam = db.execute(
        'SELECT questions_json FROM exams WHERE id = ?',
        (exam_id,)
    ).fetchone()

    questions_raw = exam['questions_json'] if exam else None
    score = None

    if questions_raw:
        try:
            questions = json.loads(questions_raw)
            answers = json.loads(job.get('answers_json', '{}')) if isinstance(job.get('answers_json'), str) else (job.get('answers_json') or {})
            score = calculate_submission_score(answers, questions)
        except Exception as e:
            logger.error(f"[Worker] Scoring error for job {job_id}: {e}")

    # 2. Insert into submissions
    answers_json_str = job.get('answers_json', '{}')
    if not isinstance(answers_json_str, str):
        answers_json_str = json.dumps(answers_json_str)

    start_time = job.get('start_time')
    if start_time and isinstance(start_time, str):
        start_time = start_time.replace('T', ' ').replace('Z', '')

    db.execute(
        'INSERT INTO submissions (exam_id, student_name, exam_number, student_class, identity_data, answers_json, score, start_time, mac_address) '
        'VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)',
        (
            exam_id,
            job.get('student_name', ''),
            job.get('exam_number', ''),
            job.get('student_class', ''),
            job.get('identity_data'),
            answers_json_str,
            score,
            start_time,
            job.get('mac_address'),
        )
    )
    db.commit()

    # 3. Store result for polling
    store_job_result(redis_client, job_id, {
        'status': 'completed',
        'score': score,
        'student_name': job.get('student_name', ''),
    })

    # 4. Broadcast via SocketIO (if available)
    try:
        from app import socketio
        if socketio:
            socketio.emit('submission_received', {
                'exam_id': exam_id,
                'student_name': job.get('student_name', ''),
                'mac_address': job.get('mac_address', ''),
                'score': score,
            }, room=f'exam_{exam_id}', namespace='/admin')
    except Exception:
        pass

    logger.info(f"[Worker] Job {job_id} completed: score={score}")


def get_queue_stats(redis_client):
    """Return current queue statistics."""
    try:
        pending = redis_client.llen(SUBMISSION_QUEUE_KEY)
        stats = redis_client.hgetall(queue_stats_key)
        return {
            'pending': pending,
            'enqueued': int(stats.get('enqueued', 0)),
            'processed': int(stats.get('processed', 0)),
            'errors': int(stats.get('errors', 0)),
        }
    except Exception:
        return {'pending': 0, 'enqueued': 0, 'processed': 0, 'errors': 0}


def start_worker(redis_client, get_db_func):
    """
    Start the submission worker as a daemon thread.
    Returns the thread object (can be stopped with thread.join()).
    """
    worker_thread = threading.Thread(
        target=process_submissions,
        args=(redis_client, get_db_func),
        daemon=True,
        name='submission-worker'
    )
    worker_thread.start()
    logger.info("[Queue] Submission worker thread started")
    return worker_thread
