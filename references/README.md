RetroTerm PETSCII References
================================

Purpose
- Capture the current baseline, decisions, and working notes so we can resume quickly.
- Provide quick commands, env flags, and known-good states for testing.

Contents
- baseline-2025-09-15.md — Snapshot of the current “good baseline” behavior and rationale
- usage.md — How to run, test, and debug (env flags, hexdumps, fonts)
- issues.md — Known issues, hypotheses, and next actions
- changelog.md — High-level changes in this effort

Branch
- petscii-good-baseline — Branch cut from the nearly-working state.

Quick TL;DR
- Legacy-first then fix: Translate PETSCII/ATASCII controls to ANSI first, THEN normalize SGR case (…M→m, …j→J).
- Ignore PETSCII 0x0F (SI). Map lone CR→CRLF for line advance.
- Font: use local Unifont OTFs for U+1FBxx glyphs. No .woff2.
- Debug: “TEXT (final)” logs reflect actual bytes sent to the browser.

