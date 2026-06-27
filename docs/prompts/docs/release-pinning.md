# Release And Consumer Pinning

Use this file for first-release tagging and the response format that
`eino-agent` can pin.

## First Release Decision

The first integration release should use the semver-style Git tag `v0.1.0`.
That tag is the preferred stable pin for `github.com/mattsp1290/eino-agent`.

Until `v0.1.0` exists on the remote, `eino-agent` may pin an immutable commit
SHA from `main` as a temporary integration pin. The commit pin must be replaced
with `v0.1.0` once the tag is pushed.

## Pin Formats

Preferred semver tag pin:

```text
github.com/mattsp1290/eino-obs v0.1.0
```

Temporary commit pin, only before `v0.1.0` is available:

```text
github.com/mattsp1290/eino-obs <full-40-character-commit-sha>
```

For Go consumers, the command form is:

```bash
go get github.com/mattsp1290/eino-obs@v0.1.0
```

Temporary commit form:

```bash
go get github.com/mattsp1290/eino-obs@<full-40-character-commit-sha>
```

After a commit pin is resolved by `go get`, `eino-agent` should record the
resulting module version from `go list -m github.com/mattsp1290/eino-obs`.
That value may be a Go pseudo-version, but the source response must still
include the original commit SHA used to derive it.

## Release Response Format For `eino-agent`

When this repository is ready for the first integration response, provide this
record to `eino-agent`:

```yaml
module: github.com/mattsp1290/eino-obs
recommended_pin:
  kind: semver-tag
  value: v0.1.0
  command: go get github.com/mattsp1290/eino-obs@v0.1.0
fallback_pin:
  kind: commit
  value: <full-40-character-commit-sha>
  command: go get github.com/mattsp1290/eino-obs@<full-40-character-commit-sha>
consumer_record:
  go_module_require: github.com/mattsp1290/eino-obs v0.1.0
  source_commit: <full-40-character-commit-sha-that-was-tagged>
  release_notes: CHANGELOG.md#v010---unreleased
```

For the final tagged response, replace `release_notes` with the dated
`CHANGELOG.md` anchor if the heading changes from `Unreleased`.

## What `eino-agent` Should Record

`eino-agent` should record all of the following in its integration notes or
dependency update:

- module path: `github.com/mattsp1290/eino-obs`;
- selected pin kind: `semver-tag` for `v0.1.0`, or `commit` for a temporary
  pre-tag pin;
- selected pin value: `v0.1.0` or the full 40-character commit SHA;
- Go module version written to `go.mod` after `go get`;
- source commit SHA associated with the tag or temporary commit pin;
- release-note reference, normally `CHANGELOG.md`;
- validation expectation: `go test ./...` in `eino-agent` must not require live
  Datadog credentials because `eino-obs` defaults to no-network behavior.

## Follow-Up

The follow-up release-response bead should replace the placeholder commit SHA
above with the exact pushed `main` commit or tagged commit and should update
`CHANGELOG.md` from `Unreleased` to the final release date if the `v0.1.0` tag
is cut in the same release pass.
