FROM cgr.dev/chainguard/static:latest
ARG TARGETARCH

# 65532 is the UID of the `nonroot` user in chainguard/static.  See: https://edu.chainguard.dev/chainguard/chainguard-images/reference/static/overview/#users
USER 65532:65532

COPY --chown=65532:65532 --chmod=0700 "build/zarf-linux-$TARGETARCH" /zarf

CMD ["/zarf", "internal", "agent", "--log-level=debug", "--log-format=text", "--no-log-file"]
