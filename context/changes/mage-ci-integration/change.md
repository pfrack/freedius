---
id: mage-ci-integration
title: "Integrate Mage into GitHub Actions CI"
status: implementing
created: 2026-07-01
updated: 2026-07-01
---

# Change: Integrate Mage into GitHub Actions CI

## Goal

Replace raw `go` commands in `.github/workflows/ci.yml` with `mage ci` (or individual mage targets) so CI uses the same build logic as local development.

## Motivation

The project already has a complete Mage setup (`magefiles/mage.go`) with a `CI` target that runs `Vet → GenerateCheck → Test → Build`. GitHub Actions currently duplicates this logic with raw `go` commands. Using Mage in CI ensures parity between local and CI pipelines.
