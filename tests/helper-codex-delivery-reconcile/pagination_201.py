"""在常规200边界用例上提供真实第201条候选页，验证不会越预算读取。"""
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path[:0] = [str(ROOT / 'tools/sessionhelper'), str(ROOT / 'tools/sessionhelper/tests')]
import test_codex_delivery_reconcile as fixture


class Pagination201(fixture.ReconcileBoundaries):
    def test_actual_201_turn_source_stops_at_200(self):
        fillers = [{'id': 'other-' + str(i), 'items': []} for i in range(199)]
        self.rpc.pages = {
            None: {'data': [self.turn] + fillers, 'nextCursor': 'native-opaque-page-201'},
            'native-opaque-page-201': {'data': [{'id': 'actual-201', 'items': []}]},
        }
        self.assertEqual(sum(len(page['data']) for page in self.rpc.pages.values()), 201)
        self.assertEqual(self.scan(), 'incomplete', 'SOURCE_201_NOT_INCOMPLETE')
        self.assertEqual(len(self.rpc.calls), 1, 'SOURCE_201_READ_BEYOND_BUDGET')
        self.assert_pending('SOURCE_201_ADVANCED_ACK')
        print('source_turns=201 queried_turns=200 native_requests=1 result=incomplete ack=unchanged')


if __name__ == '__main__':
    suite = unittest.TestSuite([Pagination201('test_actual_201_turn_source_stops_at_200')])
    raise SystemExit(not unittest.TextTestRunner(verbosity=2).run(suite).wasSuccessful())
