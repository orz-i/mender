# Binaries and web assets are produced by the pinned host toolchain. This build
# context contains only a reviewed release tree, never source secrets or .git.
ARG TRUST_IMAGE
FROM ${TRUST_IMAGE} AS trust
FROM scratch AS runtime
COPY --from=trust /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY bin/ /mender/
USER 65532:65532
WORKDIR /mender
ENTRYPOINT ["/mender/api"]

FROM trust AS web
COPY console/ /srv/console/
COPY admin/ /srv/admin/
USER 101:101
ENTRYPOINT ["nginx", "-g", "daemon off;"]
CMD []
