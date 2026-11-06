# Roadmap

## Next
- Split the inputs of a package and its test variant when the go command prints them mixed, so `-p 1` isn't needed
- Markdown output for pull request comments and job summaries
- Show the source file path for changed files, from the go command's file hash lines

## Later
- Explain test result cache misses from `GODEBUG=gocachetest=1` output, which depend on files and environment variables the tests read
- A GitHub Action wrapper that records a run and uploads the record, once `record` has been used in real CI runs
- Read Windows paths when finding the changed part of a directory
