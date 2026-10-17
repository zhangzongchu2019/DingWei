# Only a validated, credential-free prepare_context.py output is a build context.
FROM python@sha256:bc01d00b6417e4b1b01c331db3a14054b4be954e571ef8bf5748aaf64b66e941 AS builder
COPY wheels /wheels
COPY helper/requirements-runtime.lock /requirements-runtime.lock
RUN python -c 'import sys,sysconfig; assert sys.implementation.name == "cpython" and sys.version_info[:2] == (3,12); assert not sysconfig.get_config_var("Py_GIL_DISABLED")' \
    && python -m venv /opt/dingwei/sessionhelper-venv \
    && /opt/dingwei/sessionhelper-venv/bin/python -m pip install --no-index --find-links=/wheels --require-hashes --only-binary=:all: -r /requirements-runtime.lock \
    && /opt/dingwei/sessionhelper-venv/bin/python -m pip check \
    && rm -rf /opt/dingwei/sessionhelper-venv/lib/python3.12/site-packages/pip* /opt/dingwei/sessionhelper-venv/bin/pip*
COPY verify_runtime.py /verify_runtime.py
RUN /opt/dingwei/sessionhelper-venv/bin/python -I /verify_runtime.py /requirements-runtime.lock
FROM python@sha256:bc01d00b6417e4b1b01c331db3a14054b4be954e571ef8bf5748aaf64b66e941
COPY runtime-debs/*.deb /tmp/runtime-debs/
COPY security-debs/*.deb /tmp/security-debs/
RUN dpkg -i /tmp/runtime-debs/*.deb /tmp/security-debs/*.deb \
    && test "$(dpkg-query -W -f='${Version}' libpcre2-8-0)" = '10.42-1+deb12u2' \
    && rm -rf /tmp/runtime-debs /tmp/security-debs \
    && rm -rf /usr/local/bin/pip* /usr/local/lib/python3.12/ensurepip /usr/local/lib/python3.12/site-packages/pip* \
       /usr/local/lib/python3.12/site-packages/setuptools* /usr/local/lib/python3.12/site-packages/pkg_resources \
       /usr/local/lib/python3.12/site-packages/_distutils_hack /usr/local/lib/python3.12/site-packages/distutils-precedence.pth
COPY --from=builder /opt/dingwei/sessionhelper-venv /opt/dingwei/sessionhelper-venv
COPY helper/ /opt/dingwei/sessionhelper-src/
COPY native/package/ /opt/codex/
COPY start.py /usr/local/bin/hosted-start
COPY codex.py /usr/local/bin/codex
COPY dingwei-file /usr/local/bin/dingwei-file
RUN ln -s vendor/x86_64-unknown-linux-musl/bin/codex /opt/codex/codex \
    && test -f /opt/codex/vendor/x86_64-unknown-linux-musl/codex-package.json \
    && chmod 755 /usr/local/bin/hosted-start /usr/local/bin/codex /usr/local/bin/dingwei-file
ENV PYTHONPATH=/opt/dingwei/sessionhelper-src HOME=/work PYTHONDONTWRITEBYTECODE=1
ARG SOURCE_COMMIT
LABEL org.opencontainers.image.revision=$SOURCE_COMMIT dingwei.codex.version="0.159.2" dingwei.helper.version="0.2.3.4"
USER 10001:10001
WORKDIR /tmp
CMD ["/usr/local/bin/hosted-start"]
