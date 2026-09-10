# Pushing the three MeterNode repositories to MeterHome

This session could not create or push to `MeterHome/*` — the GitHub App has no
`create repository` permission on that org, and the session's repo attachment is
scoped to a single owner (`Tommyngo61`). So the three repositories are packaged
as git bundles instead: each is a complete repository, full history and tags.

## 1. Create three empty repositories

On github.com/MeterHome, create these **without** a README, .gitignore, or
licence — the bundles carry their own initial commit and an auto-init would
force you to merge unrelated histories:

- `meter-node-proto`
- `meter-node-console`
- `meter-node-agent`

Private is the right setting; `rms-frontend-portal` is private too.

## 2. Push each bundle

```bash
for r in meter-node-proto meter-node-console meter-node-agent; do
  git clone "$r.bundle" "$r"
  cd "$r"
  git remote set-url origin "git@github.com:MeterHome/$r.git"
  git push -u origin main
  git push --tags          # meter-node-proto carries v0.1.0
  cd ..
done
```

Clone them as **siblings of each other**. `meter-node-console` and
`meter-node-agent` reach the schema module through a `replace` directive
pointing at `../meter-node-proto`, and the comment in each `go.mod` says exactly
what to delete once `v0.1.0` is resolvable from the module proxy.

## 3. Verify

```bash
cd meter-node-proto   && go test ./...   # includes the bandwidth budget assertions
cd ../meter-node-console && go test ./... && make ci
cd ../meter-node-agent   && go test ./... && make ci
```

## 4. Two settings CI needs

**`METERNODE_PROTO_TOKEN`** — a repository secret on both `meter-node-console`
and `meter-node-agent`. Their workflows check out `meter-node-proto` as a
sibling, and a repository's default `GITHUB_TOKEN` cannot read another private
repo. A fine-grained PAT or GitHub App token with read access to
`MeterHome/meter-node-proto` is enough. This requirement disappears the moment
the `replace` directives do.

**Nothing else.** The integration job brings up TimescaleDB as a service
container; the agent's package job needs only `dpkg-deb`, which the runner has.

## What is in each

| | |
| --- | --- |
| `meter-node-proto` | the wire schema, tagged `v0.1.0` (schema 1.0.0) |
| `meter-node-console` | controller, M0 + M1 — migrations, enrollment, credentials, fingerprint binding, simulator |
| `meter-node-agent` | host agent, M0 + M1 — collectors, local CLI, systemd unit, .deb |
