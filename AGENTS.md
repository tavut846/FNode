# FNode AI Agent Directives & Rules

This document specifies mandatory rules for all AI agents (Antigravity, Gemini, Claude, Cursor, Copilot, etc.) working on this repository.

---

## 1. Documentation Integrity (Mandatory)

- **Always read [docs/PROJECT_GUIDE.md](docs/PROJECT_GUIDE.md)** before designing or modifying components to understand the architecture, panel interactions, and configuration specifications.
- **Always keep [docs/PROJECT_GUIDE.md](docs/PROJECT_GUIDE.md) and [docs/COMMUNICATION_FLOW_LOGIC.md](docs/COMMUNICATION_FLOW_LOGIC.md) up to date**:
  Whenever you modify, add, or delete features, node types, TLS configurations, scripts, or architectural flows, you **MUST** update the documentation in the same session.

---

## 2. Core Exclusivity

- FNode exclusively uses the **sing-box** engine (`github.com/sagernet/sing-box` / `cedar2025/sing-box`).
- **Do not** reintroduce legacy Xray or standalone Hysteria 1/2 dependencies or binaries.

---

## 3. Mandatory Testing Before Reporting Done (Strict)

- **Write & Run Verification Tests**: After making ANY change, AI agents (Antigravity, Gemini, Claude, Cursor, Copilot, etc.) **MUST** write unit/integration tests covering the change and execute them to verify that the change actually works before reporting done.
- **Never report completion without executing tests**: Verifying compilation and running tests is mandatory; never assume code works without test execution logs.
- Go version requirement: **1.25+**.
- Always build and test with the experimental JSON v2 tag and required build tags:
  ```bash
  # PowerShell:
  $env:GOEXPERIMENT="jsonv2"; go test -v -tags "with_utls" ./...

  # Linux / Bash:
  GOEXPERIMENT=jsonv2 go test -v -tags "with_utls" ./...
  ```
- Any code changes must compile without errors and pass all tests under `GOEXPERIMENT=jsonv2`.

---

## 4. Graphify Knowledge Graph

- Before answering architecture questions, consult `graphify-out/GRAPH_REPORT.md` or query graphify.
- After modifying code files in any session, execute:
  ```bash
  graphify update .
  ```
  to keep the knowledge graph in sync.

---

## 5. Commit Message Output on Confirmation (Mandatory)

- Each time you confirm and report completion of changes, you **MUST** write the commit message directly (following Conventional Commits format, e.g. `feat: ...`, `fix: ...`, with a concise summary line and descriptive bullet points) under a `### Commit Message` section at the end of your response, instead of writing git shell commands or "Recommended Git Commit" command blocks.

