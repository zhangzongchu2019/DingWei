# Hosted Codex runtime; credentials are delivered only as runtime files.
FROM python@sha256:bc01d00b6417e4b1b01c331db3a14054b4be954e571ef8bf5748aaf64b66e941 AS builder
COPY wheels /wheels
COPY requirements.lock /requirements.lock
RUN python -m pip install --no-index --find-links=/wheels --require-hashes --no-deps --target /runtime -r /requirements.lock
COPY client/tools/sessionhelper/sessionhelper /client/sessionhelper
RUN python -m compileall -q /client
FROM python@sha256:bc01d00b6417e4b1b01c331db3a14054b4be954e571ef8bf5748aaf64b66e941
COPY runtime-debs/*.deb /tmp/runtime-debs/
RUN dpkg -i /tmp/runtime-debs/*.deb && rm -rf /tmp/runtime-debs
RUN printf '%s\n' 'deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] http://deb.debian.org/debian-security bookworm-security main' > /tmp/security.list \
    && apt-get -o Dir::Etc::sourcelist=/tmp/security.list -o Dir::Etc::sourceparts=- update \
    && apt-get -o Dir::Etc::sourcelist=/tmp/security.list -o Dir::Etc::sourceparts=- install -y --no-install-recommends --only-upgrade libpcre2-8-0=10.42-1+deb12u2 \
    && test "$(dpkg-query -W -f='${Version}' libpcre2-8-0)" = '10.42-1+deb12u2' \
    && rm -f /tmp/security.list && rm -rf /var/lib/apt/lists/*
COPY --from=builder /runtime /opt/python
COPY --from=builder /client /opt/client
COPY native/package/ /opt/codex/
COPY start.py /usr/local/bin/hosted-start
COPY codex.py /usr/local/bin/codex
COPY dingwei-file /usr/local/bin/dingwei-file
RUN ln -s vendor/x86_64-unknown-linux-musl/bin/codex /opt/codex/codex \
    && test -f /opt/codex/vendor/x86_64-unknown-linux-musl/codex-package.json \
    && chmod 755 /usr/local/bin/hosted-start /usr/local/bin/codex /usr/local/bin/dingwei-file \
    && rm -rf /usr/local/bin/pip* /usr/local/lib/python3.12/ensurepip \
       /usr/local/lib/python3.12/site-packages/pip* \
       /usr/local/lib/python3.12/site-packages/setuptools* \
       /usr/local/lib/python3.12/site-packages/pkg_resources \
       /usr/local/lib/python3.12/site-packages/_distutils_hack \
       /usr/local/lib/python3.12/site-packages/distutils-precedence.pth
ENV PYTHONPATH=/opt/python:/opt/client HOME=/work
LABEL org.opencontainers.image.revision="f3abc51fdf77c6f9249822a1cb761c336ac0e701" dingwei.codex.version="0.159.2"
USER 10001:10001
WORKDIR /tmp
CMD ["/usr/local/bin/hosted-start"]
