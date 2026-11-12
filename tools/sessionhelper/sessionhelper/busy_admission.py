"""仅取消确知尚未提交、正在等待旧回合的投递。

取消与进入提交边界共用一把锁；超限后的后台 worker 不得晚到入场。
状态在 driver 生命周期内保留，取消不能因同 ID 再调用而被清除。
"""
import threading


class BusyAdmission:
    def __init__(self):
        self._lock = threading.Lock()
        self._states = {}

    def waiting(self, delivery_id):
        with self._lock:
            if self._states.get(delivery_id) not in ('cancelled', 'submitting'):
                self._states[delivery_id] = 'waiting'

    def is_waiting(self, delivery_id):
        with self._lock:
            return self._states.get(delivery_id) == 'waiting'

    def cancelled(self, delivery_id):
        with self._lock:
            return self._states.get(delivery_id) == 'cancelled'

    def cancel_waiting(self, delivery_id):
        with self._lock:
            if self._states.get(delivery_id) != 'waiting':
                return False
            self._states[delivery_id] = 'cancelled'
            return True

    def before_submit(self, delivery_id):
        with self._lock:
            if self._states.get(delivery_id) == 'cancelled':
                return False
            self._states[delivery_id] = 'submitting'
            return True
