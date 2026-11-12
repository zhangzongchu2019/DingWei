#!/usr/bin/env python3
"""用当前 Go 工具链重跑包级状态候选审计，输出 JSON 供逐项复核。"""
from pathlib import Path
import subprocess
import tempfile


def main():
    here = Path(__file__).resolve().parent
    root = here.parents[2]
    # Go 程序保存在 .txt 中，避免证据目录成为额外的 go test 包。
    with tempfile.TemporaryDirectory(prefix='hosted-global-audit-') as directory:
        program = Path(directory) / 'audit.go'
        program.write_bytes((here / 'raw/global-state-audit.go.txt').read_bytes())
        subprocess.run(['go', 'run', str(program), str(root)], cwd=root, check=True)


if __name__ == '__main__':
    main()
