# GADS Go port

A Go port of [ctrlo GADS](https://github.com/ctrlo/GADS), with a conformance
suite that runs every scenario against the original Perl and against Go and
requires identical observations.

- `gads-go/`: the Go module (application, engine, template engine, tools)
- `conformance/`: scenarios, the Perl reference runner and the Perl golden results
- `GETTING-STARTED.md`: run the servers and the tests (start here)
- `CLAUDE.md`: the workspace guide (conventions, build, test, Docker, security)

The Perl GADS is included as submodules, pinned to the commits the golden
results come from:

- `upstream/GADS-dev`: ctrlo/GADS `dev` at `fddd4a15`, the reference
- `upstream/GADS`: ctrlo/GADS `master` at `571f2bd`, the old reference

```sh
git clone --recurse-submodules https://github.com/citaylor/gads-go.git
git submodule update --init      # in an existing clone without them
```
