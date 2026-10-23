# Proposed basic runtime safety fixes

This is a separate working copy of `source-basic`. No original files, GitHub branch, release, PR or Stepik content were modified by this reviewer.

## Finding → scenario → consequence

1. **Target failure followed by POST** (`runtime/labcheck.go`, `httpChecks`): a different loopback service responds to `/version` with a wrong instance, release, source revision or API contract. The old checker records failure but continues POST `/tickets`. It can therefore mutate a local service that it has already established is not the assigned target. The patch returns after the version response and marks mandatory dependent checks `not_checked`. A correct target still reaches its write check.
2. **Docker endpoint not bound** (`docker_labs.go`, `backup_labs.go`, `select_labs.go`, shell cleanup scripts): on an otherwise supported Ubuntu VM, a remote Docker context or `DOCKER_HOST` can redirect Compose, database creation/restore, image tagging or cleanup to another daemon. OS/labels do not establish daemon location. The patch rejects endpoint/context overrides, requires default context metadata to resolve to `unix:///var/run/docker.sock`, and pins actual commands to that socket. A shell function applies the same policy before each direct Docker call, including cleanup.

## Tests prepared

`runtime/target_mutation_safety_test.go` contains six tests:

- mismatched instance/release/source/API contract sends no subsequent HTTP requests;
- confirmed target still exercises a write (positive control);
- constructed mutation command pins executable/socket independently of active context, without executing it;
- each forbidden environment override fails before even probing Docker metadata;
- remote/named contexts, wrong sockets and metadata errors fail closed;
- compose/start/reset/restore/release-change entry points reject before workload/file operations.

HTTP tests use only Go `httptest` loopback servers. Docker tests stub metadata probes or construct an `exec.Cmd` without calling Run/Start. No Docker or external network is needed for these new tests. The existing constant-ID fixture now supplies its correct API contract so its identity check passes and it continues to test uniqueness rather than failing for the new identity gate.

Portable official Go 1.27.3 became available during preparation. Its `gofmt` successfully formatted the seven changed/new Go files. **No compilation or tests were executed by this reviewer.** The parent can run the six new tests plus the existing constant-ID regression on Windows with `go test -count=1 -run '^(TestUnconfirmedTargetNeverReceivesFurtherRequests|TestConfirmedTargetStillExercisesWrite|TestMutationCommandPinsEndpointWithoutRunningIt|TestDockerOverridesRejectedWithoutProbeOrMutation|TestDockerContextAndEndpointFailClosed|TestDockerMutationEntrypointsRejectBeforeWork|TestConstantValidIDRejected)$' -v .` from `runtime/`; these tests do not execute Docker or labs. Dependencies must be fetched only through the separately authorized setup. On an isolated Ubuntu runner run `cd runtime && go test ./...` (and optionally `go test -race ./...`). Validate shell syntax with `bash -n scripts/docker-safety.sh scripts/check-container.sh scripts/check-postgres.sh qa/route-acceptance.sh` before integration. Keep all actual lab/cleanup execution on the dedicated disposable runner.

## Boundaries and integration

- The guard intentionally supports only rootful Linux Docker at `/var/run/docker.sock` and rejects rootless/Desktop/remote contexts. Unsupported environments must fail, not silently fall back or auto-switch contexts.
- A local socket alone does not prove the daemon is disposable; the operator must still use a dedicated VM/hosted runner. The patch cannot protect a shared local production daemon or a deliberately forwarded local socket.
- Labels/instance ownership checks remain necessary. Shell cleanup still deletes its own disposable named project/volume; no claim is made that it is suitable for shared hosts.
- Read-only checker probes and `doctor` retain their original routing. Mutation guards reject author SSH-probe configuration rather than using it for workload changes.
- Shell wrapper protection applies to direct `docker` commands in the reviewed shell. It does not retrofit the old original-compatible kit binary. That archive must retain its own separately scoped acceptance evidence.
- The original workflows in this copy are unchanged. **Merge only the changed-file manifest into the quality branch; do not replace its guarded workflows with these original workflow files.** Keep publication and write permissions removed in the parent quality branch.
- No source/release archive version, mutable base-image dependency, pricing, access rights or audience was changed. No PR was attempted.
