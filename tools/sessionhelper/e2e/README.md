# Isolated Linux Docker guard matrix

This runner operates **only newly created, uniquely named fixture containers**.
It never discovers or restarts real Agents. An authorized executor must have a
real Docker daemon and an immutable locally available Linux image with bash,
`/usr/bin/tini` and `/opt/dingwei/sessionhelper-venv/bin/python`. The code author
has no Docker socket permission; script delivery is not container acceptance.

Build the test-only runtime locally (no registry push), using a chosen pinned
Debian/Python base digest and the exact runtime lock from this Git checkout:

```sh
matrix_context=$(mktemp -d)
cp tools/sessionhelper/requirements-runtime.lock "$matrix_context/"
cp tools/sessionhelper/e2e/Dockerfile.matrix "$matrix_context/Dockerfile"
docker build --build-arg BASE_IMAGE="$PINNED_DEBIAN_PYTHON_BASE" -t dw-helper-matrix-local "$matrix_context"
matrix_image=$(docker image inspect --format '{{.Id}}' dw-helper-matrix-local)
python3 tools/sessionhelper/e2e/docker_matrix.py --image "$matrix_image" --evidence "$EMPTY_PRIVATE_EVIDENCE_DIRECTORY"
```

The runner requires an image ID/digest; it does not implicitly pull/build an
image. Failure to access Docker/image returns **77/BLOCK**, never PASS. Actual
matrix failure returns1. All fixture source, env and Agent IDs are synthetic.
It mounts the stable launcher/factory read-only at the real pilot mount point,
uses the unchanged tini/bash/source-env/python entry, mounts private writable
state and verifies survival across real Docker restarts. It exercises no-job
factory bootstrap, A confirmation, B trial restart/rejection/rollback to A,
fresh C confirmation, startup crash, actual 120-second health timeout, a separate consecutive A2→B2→C2 sequence with real restarts and C2 budget rollback to B2, terminal anti-loop. A run takes several minutes; allow at least ten minutes.
A completion requires RESULT.json plus Docker logs, not merely exit from a mock.
Use --keep only for private debugging; default removes only its own container
and temporary fixture tree. No Docker permission workaround is provided.

**Boundary:** candidates and health-confirmation calls are deterministic local
fixtures around the production stdlib guard, not an authenticated Hub/helper
E2E. Real Hub offers, downloads/SHA-negative, live fresh-session confirmation,
real driver/IPC readiness, packaging/bootstrap preflight remain additional feature gates. Existing host/core tests
cover SHA/lock rejection; do not report those as this container matrix.
