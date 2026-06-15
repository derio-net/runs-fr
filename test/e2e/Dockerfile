# runs-fr in-cluster e2e smoke image (ghcr.io/derio-net/runs-fr-smoke). Built per-commit by
# .github/workflows/build.yml; run as a Job in the staging vCluster by the Frank staging-gate.
FROM python:3.12-slim
RUN pip install --no-cache-dir "websockets==12.0" "kubernetes==31.0.0"
COPY smoke.py /smoke.py
USER 65532:65532
ENTRYPOINT ["python", "/smoke.py"]
