# Native code server v3

This service replaces the Python HTTP control process on port 49999 with a
small static Go server. It waits for envd on port 49983 before opening the
public listener, then caches the successful envd readiness result. This keeps
the Template readiness contract intact without issuing another envd health
request for every Cubelet probe.

`POST /execute` preserves the image NDJSON response protocol and runs each
request in a fresh Python subprocess. The unit tests cover readiness gating,
the short wait path, NDJSON output, Python result capture, and malformed
requests.

With the later cross-stage early probe disabled, the validated 2U2G matrix was
c1/n20 39.000 ms, c10/n200 53.845 ms, c20/n300 58.695 ms, and c50/n500
138.610 ms average latency. Four c50/n500 rounds averaged 138.631 ms with
2000/2000 successful creations and SDK `run_code` passing 3/3.

The early-probe systemd drop-in is intentionally not included: its mean c50
gain was only 3.22%, and it was explicitly rolled back to keep the readiness
path simpler.
