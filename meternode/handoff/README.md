# Handoff bundles

The same three components as the directories beside this one, repackaged as the
standalone repositories they are meant to become:

| here | becomes |
| --- | --- |
| `meter-node-proto.bundle` | `github.com/MeterHome/meter-node-proto` (tagged `v0.1.0`) |
| `meter-node-console.bundle` | `github.com/MeterHome/meter-node-console` |
| `meter-node-agent.bundle` | `github.com/MeterHome/meter-node-agent` |

They differ from `../meternode-*` only in packaging: module paths are
`github.com/MeterHome/meter-node-*`, the sibling `replace` directives point at
the new names, Docker build contexts and CI workflows are rewritten for
standalone repos, and cross-repo documentation links are real GitHub URLs.

Each bundle is a complete repository with its own history. Read
[`PUSH.md`](PUSH.md) for the four steps.

Delete this directory once the repositories exist.
