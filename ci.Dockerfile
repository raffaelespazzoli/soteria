# ci.Dockerfile — used by redhat-cop pipeline (pre-built binary)
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

LABEL name="soteria" \
      summary="Soteria DR Orchestrator" \
      description="Storage-agnostic disaster recovery orchestrator for OpenShift Virtualization" \
      vendor="Soteria Project" \
      io.k8s.display-name="Soteria DR Orchestrator" \
      io.k8s.description="Storage-agnostic disaster recovery orchestrator for OpenShift Virtualization" \
      io.openshift.tags="disaster-recovery,openshift-virtualization,dr"

COPY bin/manager /manager

USER 65532:65532

ENTRYPOINT ["/manager"]
