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
COPY native/package/ /opt/claude/
COPY codex_start.py /opt/dingwei/codex_start.py
COPY claude-start.py /usr/local/bin/hosted-claude-start
COPY claude.py /usr/local/bin/claude
RUN test -x /opt/claude/claude \
    && chmod 755 /usr/local/bin/hosted-claude-start /usr/local/bin/claude
ENV PYTHONPATH=/opt/dingwei/sessionhelper-src HOME=/work PYTHONDONTWRITEBYTECODE=1
ARG SOURCE_COMMIT
LABEL org.opencontainers.image.revision=$SOURCE_COMMIT dingwei.claude.version="2.1.285" dingwei.helper.version="0.2.3.4"
USER 10001:10001
WORKDIR /tmp
CMD ["/usr/local/bin/hosted-claude-start"]
