---
id: quality-gates-in-ci
title: "Audit and harden quality gates in CI"
status: implemented
created: 2026-07-02
updated: 2026-07-02
---

# Change: Audit and harden quality gates in CI

## Goal

Audit every quality gate currently enforced by `mage ci`, `.github/workflows/ci.yml`, and `scripts/pre-commit`; identify gaps against a healthy Go CI baseline; and propose a plan to close them without hurting feedback speed or local/CI parity.

## Motivation

The recent `mage-ci-integration` change moved the pipeline to `mage ci`, but nobody has since checked which gates the pipeline actually enforces vs which are documented, which are duplicated, and which are missing. Before adding more gates (coverage threshold, format check, mod-verify, etc.) we need a single truthful reference point.
