# Build the manager binary on the UBI9 Go toolset.
FROM registry.access.redhat.com/ubi9/go-toolset:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH
# Allow Go to fetch the exact toolchain named in go.mod if the base image's Go
# differs slightly; harmless when it already matches.
ENV GOTOOLCHAIN=auto

WORKDIR /opt/app-root/src
# Copy the Go module manifests first and cache dependency downloads. The
# go-toolset image runs as UID 1001 in group 0, so files are chowned to match.
COPY --chown=1001:0 go.mod go.mod
COPY --chown=1001:0 go.sum go.sum
RUN go mod download

# Copy the go source.
COPY --chown=1001:0 cmd/ cmd/
COPY --chown=1001:0 api/ api/
COPY --chown=1001:0 internal/ internal/

# Build a static binary so it runs on a minimal UBI runtime.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -a -o manager cmd/main.go

# Runtime: UBI9 minimal. ca-certificates is required for the operator's outbound
# HTTPS (e.g. the Steam version poll).
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
ARG VERSION=0.1.0
LABEL name="palworld-operator" \
      vendor="twodcube" \
      version="${VERSION}" \
      summary="Palworld dedicated server operator" \
      description="Kubernetes/OpenShift operator for hosting Palworld dedicated servers" \
      io.k8s.description="Kubernetes/OpenShift operator for hosting Palworld dedicated servers" \
      io.k8s.display-name="Palworld Operator" \
      io.openshift.tags="palworld,game-server,operator"

RUN microdnf install -y ca-certificates && microdnf clean all

WORKDIR /
COPY --from=builder /opt/app-root/src/manager /manager
# Non-root by default; OpenShift overrides this with an arbitrary UID.
USER 65532:65532

ENTRYPOINT ["/manager"]
