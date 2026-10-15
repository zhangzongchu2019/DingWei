"""Installed as the read-only root sessionhelper/__main__.py."""
from pathlib import Path
import sys

root = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(root))
from helper_update_guard import main

if __name__ == '__main__':
    raise SystemExit(main(root / 'factory'))
