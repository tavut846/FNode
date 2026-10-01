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

## 3. Go Build & Test Environment

- Go version requirement: **1.25+**.
- Always build and test with the experimental JSON v2 tag:
  ```bash
  GOEXPERIMENT=jsonv2 go test ./...
  ```
- Any code changes must compile without errors under `GOEXPERIMENT=jsonv2`.

---

## 4. Graphify Knowledge Graph

- Before answering architecture questions, consult `graphify-out/GRAPH_REPORT.md` or query graphify.
- After modifying code files in any session, execute:
  ```bash
  graphify update .
  ```
  to keep the knowledge graph in sync.
