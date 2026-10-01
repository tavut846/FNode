# Mandatory Testing & Verification Rule

This rule applies to all AI agents (Antigravity, Gemini, Claude, Cursor, Copilot, etc.) working on the FNode codebase.

## Core Directives

1. **Write Tests for Every Change**:
   - Whenever you create, modify, or refactor any code, configuration mapping, or protocol handler, you **MUST** write or update unit/integration tests covering the change.
   - Do not rely on assumptions or static analysis alone.

2. **Execute Tests and Verify Output**:
   - Before reporting a task as complete or done, you **MUST** execute the tests in the shell and inspect the output.
   - Required test command:
     - **PowerShell (Windows)**:
       ```powershell
       $env:GOEXPERIMENT="jsonv2"; go test -v -tags "with_utls" ./...
       ```
     - **Bash / Linux / macOS**:
       ```bash
       GOEXPERIMENT=jsonv2 go test -v -tags "with_utls" ./...
       ```
   - If only a specific package was modified, package-level testing is acceptable during iteration (e.g. `go test -v -tags "with_utls" ./core/sing`), but a full pass across `./...` must be verified before declaring the task done.

3. **Strict Completion Gate**:
   - **NEVER** declare or report a task done without having run tests verifying that the change compiles, passes, and actually functions.
   - If any test fails, diagnose and resolve the failure before reporting back to the user.
