![Trimness banner](assets/trimness-banner.png)

# trimness ⚡

**An experimental agent harness and infrastructure research project, built in Go.**

*A foundation for investigating how agents use context, tools, and skills.*

![Go](https://img.shields.io/badge/Go-00ADD8?style=flat-square&logo=go&logoColor=white) ![Status](https://img.shields.io/badge/status-WIP-555555?style=flat-square)

---

### About

Trimness is an experimental agent harness built from scratch in **Go**. It's not a claim that these problems are solved. It's a foundation for investigating them in a controlled, measurable way.

The overarching theme is **agent infrastructure engineering**: the systems around the model that determine how well an agent performs, what it costs, and how it behaves. Token optimization is one research area among several.

### Research areas

| Area | Focus | Questions |
| --- | --- | --- |
| **Context efficiency** | Token usage, compression, retrieval | How do we filter noisy tool output and unnecessary context? Can targeted edits replace whole-file rewrites? |
| **Agent-native tools** | MCP discovery, selection, execution | MCP is already built to support agents. Can discovery, selection, and use be made more efficient or adaptive? |
| **Skills architecture** | Loading, composition, activation | Skills already exist. When and how should they be loaded, composed, and activated to avoid wasted context? |
| **Execution systems** | Agent loops, state, orchestration | How should loops, state, and orchestration be structured so behavior is observable and reproducible? |

### How it works

**User → Agent → LLM → Tools → Results → Agent**

The initial harness is a coding agent that can read files, search code, make edits, and run tests. It serves as the testbed: instrumented so each research area can be explored by changing one part of the loop and measuring the effect.

### Usage

```sh
export GEMINI_API_KEY=...   # or put it in .env
go run ./cmd/trimness            # resume the most recent session (or start a new one)
go run ./cmd/trimness --session <id>
```

| Key | Action |
| --- | --- |
| `enter` / `alt+enter` | send / newline |
| `esc` | cancel the in-flight reply |
| `ctrl+n` / `ctrl+o` | new session / session picker |
| `pgup` `pgdn` / mouse wheel | scroll history |
| `ctrl+c` / `ctrl+d` | cancel, then quit / quit |

Sessions are JSON files in `$XDG_DATA_HOME/trimness/sessions` (default `~/.local/share/trimness/sessions`).

### Roadmap

- [ ] **01 / Harness** — Build the core agent loop and coding tools.
- [ ] **02 / Observe** — Track tokens, cost, and execution traces.
- [ ] **03 / Context** — Experiment with tool observations and edit strategies.
- [ ] **04 / Tools & skills** — Explore MCP and skill discovery, loading, and selection.
- [ ] **05 / Evaluate** — Benchmark and publish findings, including negative results.

### Philosophy

> Less isn't always better. Measure what matters.

Trimness prioritizes **cost per successful task**, not just fewer tokens. These are open questions under investigation, and feedback from people working on similar problems is welcome.

---

**Built with Go. Designed for experimentation.**
