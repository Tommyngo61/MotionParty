# Pushing Meter-Node to MeterHome

This session cannot create or push to `MeterHome/*` — the GitHub App has no
create-repository permission on that org, and the session's repo attachment is
scoped to a single owner (`Tommyngo61`). So the repository is packaged as a git
bundle: a complete repo, full history.

## 1. Create the repository

On github.com/MeterHome, create **`Meter-Node`** — private, and **without** a
README, .gitignore or licence. The bundle carries its own initial commit, and an
auto-init would force you to merge unrelated histories.

## 2. Push

```bash
git clone Meter-Node.bundle Meter-Node
cd Meter-Node
git remote set-url origin git@github.com:MeterHome/Meter-Node.git
git push -u origin main
```

That's it. No workspace file to configure, no cross-repo token, no `replace`
directive to remember to delete.

## 3. Verify

```bash
make test     # every unit test: controller, agent, and the shared schema
make ci       # what CI runs that needs no database
make budget   # prints the projected control-plane traffic per node
```

## What CI needs

Nothing. One checkout, one module. The integration job brings up TimescaleDB as
a service container; the package job needs only `dpkg-deb`, which the runner
has.

## Layout

```
proto/     the wire schema both sides import
console/   the control plane
agent/     the host software
```

One Go module at the root. `console/internal/...` is importable only from
`console/...` and `agent/internal/...` only from `agent/...` — Go's `internal`
rule enforces the boundary between the two components, and the compiler proves
it.
