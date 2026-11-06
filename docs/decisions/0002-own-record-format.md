# 0002: Compare only records made by `whyrebuilt record`

**Decision:** `compare` reads only files written by `record`, which wrap the go command's `GODEBUG=gocachehash=1` output with a header and an end line. Raw `gocachehash` output is refused.

**Why:** raw output can't show that the run finished or succeeded. A log cut off between two keys looks like a smaller build, and a failed build still prints the keys it computed before it stopped. Either one could be reported as a clean match or as a wrong cause. The end line holds the exit code and the line count, so a failed, cut-off or edited run is refused instead.

**Alternatives:** accept raw logs with a warning. Rejected because a warning on stderr is easy to miss when an agent or a CI step reads only the result.
