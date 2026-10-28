# GADS Go port

A Go port of [ctrlo GADS](https://github.com/ctrlo/GADS), with a conformance
suite that runs every scenario against the original Perl and against Go and
requires identical observations.

- `gads-go/`: the Go module (application, engine, template engine, tools)
- `conformance/`: scenarios, the Perl reference runner and the Perl golden results
- `CLAUDE.md`: the workspace guide (conventions, build, test, Docker, security)

The Perl GADS is not part of this repository. Clone it beside the other
directories before running the suite or building the Docker images:

```sh
git clone https://github.com/ctrlo/GADS.git upstream/GADS-dev
git -C upstream/GADS-dev checkout fddd4a15     # the dev commit the golden results come from
git clone https://github.com/ctrlo/GADS.git upstream/GADS
git -C upstream/GADS checkout 571f2bd          # old master reference (optional)
```
