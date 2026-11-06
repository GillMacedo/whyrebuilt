# Architecture

    go command --> record (GODEBUG=gocachehash=1, keep build/vet/link HASH lines and messages) --> record file (.rec or .rec.gz)
    two records --> ParseRecord (header, end line, line count, exit code, error lines) --> actions by name
                --> Compare (pair by name, diff inputs by slot) --> own changes and dependency links
                --> describe (causes) --> resolve (because of them) --> text or JSON

- **What the go command prints.** For each cache key it computes, `cmd/go/internal/cache` prints `HASH[name]` when the hash starts, `HASH[name]: "input"` for every write, and `HASH[name]: <hex>` for the result. The first write is the go command's version (the hash salt). Compile keys are named `build IMPORTPATH`, vet keys `vet IMPORTPATH` and link keys `link IMPORTPATH`. The inputs are written in `buildActionID`, `vetAction` and `linkActionID` in `cmd/go/internal/work/exec.go`.
- **Record.** `record` adds `gocachehash=1` to `GODEBUG`, keeping what is already there, and runs the command. It keeps the HASH lines of `build`, `vet` and `link` keys and every non-HASH line, and drops the module index, file sums and test result hashes, which share one name across many computations. It writes a header with the command, directory, start time and `GOCACHE` entry count, and an end line with the exit code, the number of saved lines and the entry count after. A `.gz` name writes gzip.
- **Checks.** A record without its first line or its end line, with a different number of lines than the end line says, with a damaged HASH line, with an input outside its key or a key that never finished, with a non-zero exit code, with a compiler or vet error position in its stderr, or with no keys at all, is refused. So are a cut-off gzip stream, the same recording on both sides, and two runs with no step in common.
- **Pairing.** Actions are paired by name. A name can appear more than once in a run (a package and its test variant). Equal keys pair first, then the pair that shares the most inputs. When two hashes with the same name were open at the same time, their inputs are mixed in the output. Such names are compared by their set of keys: equal sets count as the same, and different sets are reported as not explained.
- **Slots.** Each input line maps to a slot, such as `dir`, `compile`, `linkflags`, `file NAME` or `import PATH`, so that the two runs' values for the same input can be lined up. Inputs that hold a dependency's output ID (`import PATH ID`, `packagefile PATH=ID`, `packagemain`, `pkg`, `vetout PATH`) are links to another step, not causes.
- **Causes.** `describe` maps each own change to a cause with a title and advice. A different Go version hides the action's other changes, because tool IDs, standard library files and defaults all differ with it. Each step's causes are its own plus those inherited through changed dependencies. A dependency built in only one run is its own cause.
- **Notes.** When B's `GOCACHE` had no entries when B started, the report says that B rebuilt everything whatever the keys. Different commands and different cache paths are noted too.

Key choices are in `decisions/`.
