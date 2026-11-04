# Resource Limit audit - 2026-11-04

Live version 4.0.82; local pagination fix 4.0.83, not deployed.

Chrome checks: created a disabled draft for qa-placeholder-renamed.litimcsellcop.com (CPU 25%, memory 256 MiB, disk 1024 MiB), reopened Edit and changed CPU to 30%. Listing reflected the change and displayed sampled usage. Draft retained; no removal performed.

Confirmed bug: after saving, the handler enabled every button, including Previous and Next on a single-page list. Recompute picker/list control state after saving, respect busy state in pagination, and clamp/guard page boundaries.

Two frontend regression tests, JS syntax and focused Go resource tests passed. Existing Makefile retained. No build, package or deployment.

Important limitation: this feature currently stores disabled settings only. Server reports missing project quota support, and runtime isolation is not integrated in the implementation. Live usage is observational, not proof of enforcement. No quota, mount, service isolation or stress test was applied; CPU/RAM/disk containment and recovery are not production-ready.
