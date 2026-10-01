## FNode Architecture & Context

- Always read [docs/PROJECT_GUIDE.md](docs/PROJECT_GUIDE.md) to understand the project architecture, Xboard panel API interactions, sing-box core integration, FNode-script management, TLS certificate handling (including Caddy integration), and configuration rules.
- Maintain sing-box core exclusivity; do not reintroduce legacy Xray or standalone Hysteria dependencies.
- Ensure all Go code builds and passes tests using `GOEXPERIMENT=jsonv2`.
- Mandatory: Always keep [docs/PROJECT_GUIDE.md](docs/PROJECT_GUIDE.md) and related docs up to date whenever modifying or adding features, node types, configurations, scripts, or architecture.

## graphify

This project has a graphify knowledge graph at graphify-out/.

Rules:
- Before answering architecture or codebase questions, read graphify-out/GRAPH_REPORT.md for god nodes and community structure
- If graphify-out/wiki/index.md exists, navigate it instead of reading raw files
- For cross-module "how does X relate to Y" questions, prefer `graphify query "<question>"`, `graphify path "<A>" "<B>"`, or `graphify explain "<concept>"` over grep — these traverse the graph's EXTRACTED + INFERRED edges instead of scanning files
- After modifying code files in this session, run `graphify update .` to keep the graph current (AST-only, no API cost)
