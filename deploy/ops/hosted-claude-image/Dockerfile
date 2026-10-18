# Definition only: this is built later by the approved isolated Engine on host 43.
# Its input directory is produced by prepare_context.py; it never accepts a live
# home directory, a credential file, or an online package install.
FROM python@sha256:bc01d00b6417e4b1b01c331db3a14054b4be954e571ef8bf5748aaf64b66e941 AS builder
COPY wheels /wheels
COPY helper/requirements-runtime.lock /requirements-runtime.lock
RUN python -c 'import sys,sysconfig; assert sys.implementation.name == "cpython" and sys.version_info[:2] == (3,12); assert not sysconfig.get_config_var("Py_GIL_DISABLED")' \
    && python -m venv /opt/dingwei/sessionhelper-venv \
    && /opt/dingwei/sessionhelper-venv/bin/python -m pip install --no-index --find-links=/wheels --require-hashes --only-binary=:all: -r /requirements-runtime.lock \
    && /opt/dingwei/sessionhelper-venv/bin/python -m pip check
FROM python@sha256:bc01d00b6417e4b1b01c331db3a14054b4be954e571ef8bf5748aaf64b66e941
COPY runtime-debs/*.deb /tmp/runtime-debs/
COPY security-debs/*.deb /tmp/security-debs/
RUN dpkg -i /tmp/runtime-debs/*.deb /tmp/security-debs/*.deb \
    && test "$(dpkg-query -W -f='${Version}' libpcre2-8-0)" = '10.42-1+deb12u2' \
    && rm -rf /tmp/runtime-debs /tmp/security-debs
COPY --from=builder /opt/dingwei/sessionhelper-venv /opt/dingwei/sessionhelper-venv
COPY helper/ /opt/dingwei/sessionhelper-src/
COPY node/node-v22.23.3-linux-x64.tar.xz /tmp/node.tar.xz
RUN mkdir /opt/node-unpack \
    && python -c 'import hashlib,io,sys,tarfile; data=open(sys.argv[1],"rb").read(); hashlib.sha256(data).hexdigest()==sys.argv[3] or sys.exit("NODE_SHA256"); archive=tarfile.open(fileobj=io.BytesIO(data),mode="r:xz"); archive.extractall(sys.argv[2],filter="data"); archive.close()' /tmp/node.tar.xz /opt/node-unpack df450af89261115ef9f9e3830c3eeb2cc9213b63c720b1af623cb5dcbe2e02de \
    && mv /opt/node-unpack/node-v22.23.3-linux-x64 /opt/node \
    && rm -rf /tmp/node.tar.xz /opt/node-unpack
COPY claude/package/ /opt/claude/node_modules/@anthropic-ai/claude-code/
COPY native/package/ /opt/claude/node_modules/@anthropic-ai/claude-code-linux-x64/
COPY start.py /opt/dingwei/codex_start.py
COPY claude-start.py /usr/local/bin/hosted-claude-start
COPY claude.py /usr/local/bin/claude
RUN test -f /opt/claude/node_modules/@anthropic-ai/claude-code/bin/claude.exe \
    && test -x /opt/node/bin/node \
    && chmod 755 /usr/local/bin/hosted-claude-start /usr/local/bin/claude
ENV PYTHONPATH=/opt/dingwei/sessionhelper-src HOME=/work PYTHONDONTWRITEBYTECODE=1 PATH=/opt/node/bin:$PATH
ARG SOURCE_COMMIT
LABEL org.opencontainers.image.revision=$SOURCE_COMMIT dingwei.claude.version="2.1.285" dingwei.helper.version="0.2.3.4"
USER 10001:10001
WORKDIR /tmp
CMD ["/usr/local/bin/hosted-claude-start"]
