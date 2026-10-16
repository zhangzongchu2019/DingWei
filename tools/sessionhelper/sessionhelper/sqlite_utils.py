"""Transaction-scoped SQLite connections with deterministic ownership."""

from contextlib import contextmanager
import sqlite3


@contextmanager
def connection(database, *, row_factory=None, **kwargs):
    """Preserve sqlite3 commit/rollback behavior and close on every exit path."""
    db = sqlite3.connect(database, **kwargs)
    try:
        if row_factory is not None:
            db.row_factory = row_factory
        with db:
            yield db
    finally:
        db.close()
