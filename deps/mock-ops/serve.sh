#!/bin/sh
# A pretend operations API for the example actions in deps/seed.sql to call.
# busybox httpd runs cgi-bin/ops for every path under /cgi-bin/ops, which
# keeps its state in /tmp/ops.
set -u
mkdir -p /tmp/ops/jobs /tmp/ops/incidents
exec httpd -f -v -p 8080 -h /app
