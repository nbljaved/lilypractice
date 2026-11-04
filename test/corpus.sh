#!/bin/sh
# Checks lilypractice against many real scores: runs lpcheck on every piece
# in testdata/mutopia, several at a time, and sums up. Per-piece logs go
# to out/corpus/logs. Run from anywhere:
#
#	test/corpus.sh [pattern]   # only the pieces whose directory matches
#
# Needs a built lpcheck (go build -o out/bin/ ./cmd/...).
dir=$(cd "$(dirname "$0")/.." && pwd)
exec guix shell -m "$dir/manifest.scm" -- sh -c '
	dir=$1 pattern=$2
	logs=$dir/out/corpus/logs
	rm -rf "$logs" && mkdir -p "$logs"
	tail -n +2 "$dir/testdata/mutopia/index.tsv" | cut -f1,2 | grep -- "^[^	]*${pattern}" |
		xargs -P "$(nproc)" -L 1 sh -c "
			timeout 300 \"$dir/out/bin/lpcheck\" -out \"$dir/out/corpus/build\" \"$dir/testdata/mutopia/\$0/\$1\" >\"$logs/\$0.log\" 2>&1
			echo \$? >\"$logs/\$0.status\""
	cd "$logs" || exit 1
	total=$(ls *.status | wc -l)
	ok=$(grep -lx 0 *.status | wc -l)
	echo "$ok of $total pieces pass"
	echo "updated with convert-ly: $(grep -l "updated with convert-ly" *.log | wc -l)"
	echo "LilyPond went on after errors: $(grep -l "WARNING: LilyPond went on" *.log | wc -l)"
	echo
	echo "Failures, by their first problem:"
	for f in $(grep -Lx 0 *.status); do
		grep -m1 "PROBLEM" "${f%.status}.log" | sed -E "s/[0-9]+/N/g" | cut -c1-100
	done | sort | uniq -c | sort -rn
	echo
	# Failures already understood are listed in known.tsv; others are new.
	known=$dir/testdata/mutopia/known.tsv
	for f in $(grep -Lx 0 *.status); do
		piece=${f%.status}
		why=$(awk -F "\t" -v p="$piece" "\$1 == p {print \$2}" "$known")
		if [ -n "$why" ]; then
			printf "known %-38s %s\n" "$piece" "$why"
		else
			printf "NEW   %-38s %s\n" "$piece" "$(grep -m1 PROBLEM "$piece.log" | cut -c12-110)"
		fi
	done | sort
' sh "$dir" "$1"
