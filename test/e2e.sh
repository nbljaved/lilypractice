#!/bin/sh
# End-to-end tests: the real program on a virtual X display, with no sound
# (FluidSynth writes to a file). Run from anywhere:
#
#	test/e2e.sh
#
# Needs a built program (go build -o out/bin/ ./cmd/...).
dir=$(cd "$(dirname "$0")/.." && pwd)
exec guix shell -m "$dir/manifest.scm" xvfb-run xdotool procps -- \
	xvfb-run -a -s "-screen 0 1280x960x24" sh "$dir/test/e2e-run.sh" "$dir" "$dir/out/e2e"
