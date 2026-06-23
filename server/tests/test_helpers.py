"""Tests for EXAMVAN helpers (scoring engine, formatters)."""
import sys, os, json, unittest
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))
from helpers import (
    localize_date_string, format_iso_utc, get_local_ip,
    _normalize_q_num, _evaluate_single_question,
    evaluate_answers_detailed, calculate_submission_score,
)


class TestNormalizeQNum(unittest.TestCase):
    def test_integer_float(self):
        self.assertEqual(_normalize_q_num(1.0), '1')

    def test_integer_int(self):
        self.assertEqual(_normalize_q_num(1), '1')

    def test_integer_string(self):
        self.assertEqual(_normalize_q_num('1'), '1')

    def test_float_string(self):
        self.assertEqual(_normalize_q_num('1.5'), '1.5')

    def test_non_float_string(self):
        self.assertEqual(_normalize_q_num('abc'), 'abc')


class TestEvaluateSingleQuestion(unittest.TestCase):
    def test_single_choice_correct(self):
        earned, text, cls = _evaluate_single_question('A', 'A', 'single_choice', 1.0, False)
        self.assertEqual(earned, 1.0)
        self.assertEqual(text, 'correct')
        self.assertEqual(cls, 'correct')

    def test_single_choice_wrong(self):
        earned, text, cls = _evaluate_single_question('A', 'B', 'single_choice', 1.0, False)
        self.assertEqual(earned, 0.0)
        self.assertEqual(text, 'incorrect')
        self.assertEqual(cls, 'incorrect')

    def test_single_choice_case_insensitive(self):
        earned, _, _ = _evaluate_single_question('a', 'A', 'single_choice', 1.0, False)
        self.assertEqual(earned, 1.0)

    def test_true_false_correct(self):
        earned, _, cls = _evaluate_single_question('TRUE', 'TRUE', 'true_false', 1.0, False)
        self.assertEqual(earned, 1.0)
        self.assertEqual(cls, 'correct')

    def test_short_answer_correct(self):
        earned, _, _ = _evaluate_single_question('Jakarta', 'JAKARTA', 'short_answer', 2.0, False)
        self.assertEqual(earned, 2.0)

    def test_short_answer_trimmed(self):
        earned, _, _ = _evaluate_single_question('  jakarta ', 'JAKARTA', 'short_answer', 1.0, False)
        self.assertEqual(earned, 1.0)

    def test_multiple_choice_exact(self):
        earned, _, cls = _evaluate_single_question(['A', 'B'], ['A', 'B'], 'multiple_choice', 2.0, False)
        self.assertEqual(earned, 2.0)
        self.assertEqual(cls, 'correct')

    def test_multiple_choice_unordered(self):
        earned, _, cls = _evaluate_single_question(['B', 'A'], ['A', 'B'], 'multiple_choice', 2.0, False)
        self.assertEqual(earned, 2.0)

    def test_multiple_choice_partial(self):
        earned, _, cls = _evaluate_single_question(['A'], ['A', 'B'], 'multiple_choice', 2.0, True)
        self.assertEqual(earned, 1.0)  # 1/2 correct
        self.assertEqual(cls, 'partial')

    def test_multiple_choice_partial_with_wrong(self):
        earned, _, _ = _evaluate_single_question(['A', 'C'], ['A', 'B'], 'multiple_choice', 2.0, True)
        self.assertEqual(earned, 0.0)  # 1 correct, 1 wrong = 0

    def test_matching_exact(self):
        student = {'1': 'A', '2': 'B'}
        correct = {'1': 'A', '2': 'B'}
        earned, _, cls = _evaluate_single_question(student, correct, 'matching', 3.0, False)
        self.assertEqual(earned, 3.0)
        self.assertEqual(cls, 'correct')

    def test_matching_partial(self):
        student = {'1': 'A', '2': 'C'}
        correct = {'1': 'A', '2': 'B'}
        earned, _, cls = _evaluate_single_question(student, correct, 'matching', 2.0, True)
        self.assertEqual(earned, 1.0)  # 1/2 correct
        self.assertEqual(cls, 'partial')

    def test_unanswered(self):
        earned, text, cls = _evaluate_single_question(None, 'A', 'single_choice', 1.0, False)
        self.assertEqual(earned, 0.0)
        self.assertEqual(text, 'unanswered')
        self.assertEqual(cls, 'unanswered')


class TestEvaluateAnswersDetailed(unittest.TestCase):
    def test_empty_questions(self):
        self.assertEqual(evaluate_answers_detailed({}, []), {})

    def test_mixed_questions(self):
        answers = {'1': 'A', '2': 'C', '3': 'X'}
        questions = [
            {'number': 1, 'type': 'single_choice', 'key': 'A', 'weight': 1},
            {'number': 2, 'type': 'single_choice', 'key': 'B', 'weight': 2},
            {'number': 3, 'type': 'single_choice', 'key': 'Y', 'weight': 1},
        ]
        result = evaluate_answers_detailed(answers, questions)
        self.assertEqual(result['1']['statusClass'], 'correct')
        self.assertEqual(result['1']['earned'], 1.0)
        self.assertEqual(result['2']['statusClass'], 'incorrect')
        self.assertEqual(result['3']['statusClass'], 'incorrect')


class TestCalculateScore(unittest.TestCase):
    def test_none_for_empty(self):
        self.assertIsNone(calculate_submission_score({}, []))

    def test_full_score(self):
        score = calculate_submission_score(
            {'1': 'A', '2': 'B'},
            [
                {'number': 1, 'type': 'single_choice', 'key': 'A', 'weight': 1},
                {'number': 2, 'type': 'single_choice', 'key': 'B', 'weight': 2},
            ]
        )
        self.assertEqual(score, 3.0)

    def test_partial_score(self):
        score = calculate_submission_score(
            {'1': 'A', '2': 'C'},
            [
                {'number': 1, 'type': 'single_choice', 'key': 'A', 'weight': 1},
                {'number': 2, 'type': 'single_choice', 'key': 'B', 'weight': 2},
            ]
        )
        self.assertEqual(score, 1.0)


class TestFormatters(unittest.TestCase):
    def test_localize_date_none(self):
        self.assertEqual(localize_date_string(None), '—')

    def test_localize_date_utc_to_wib(self):
        # UTC+7 = WIB, offset -420 minutes from browser perspective
        result = localize_date_string('2026-06-22 10:00:00', -420)
        self.assertEqual(result, '2026-06-22 17:00:00')

    def test_localize_date_no_offset(self):
        result = localize_date_string('2026-06-22 10:00:00')
        self.assertIn('2026-06-22', result)
        self.assertIn('UTC', result)

    def test_format_iso_utc(self):
        self.assertEqual(format_iso_utc('2026-06-22 10:00:00'), '2026-06-22T10:00:00Z')

    def test_format_iso_utc_none(self):
        self.assertIsNone(format_iso_utc(None))

    def test_format_iso_utc_already_iso(self):
        self.assertEqual(format_iso_utc('2026-06-22T10:00:00Z'), '2026-06-22T10:00:00Z')

    def test_get_local_ip(self):
        ip = get_local_ip()
        self.assertTrue(len(ip) > 0)
        self.assertNotEqual(ip, '')


if __name__ == '__main__':
    unittest.main()
