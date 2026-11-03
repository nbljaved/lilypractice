#!/bin/sh
# Runs lilypractice in its Guix environment, building it first if needed:
#
#	./run.sh [flags] piece.ly
#
# LD_LIBRARY_PATH is dropped: libraries from another distribution in it
# (such as Nix's alsa-lib) break Guix programs.
dir=$(cd "$(dirname "$0")" && pwd)
exec env -u LD_LIBRARY_PATH guix shell -m "$dir/manifest.scm" -- sh -c '
	dir=$1
	shift
	# Quiet unless it fails: the GTK bindings always warn while compiling.
	log=$(go -C "$dir" build -o "$dir/out/bin/" ./cmd/... 2>&1) || {
		echo "$log" >&2
		exit 1
	}
	exec "$dir/out/bin/lilypractice" "$@"' sh "$dir" "$@"
