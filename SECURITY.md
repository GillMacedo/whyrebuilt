# Security

Report vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/GillMacedo/whyrebuilt/security/advisories/new), not in a public issue.

`whyrebuilt record` runs the command you give it, with `GODEBUG=gocachehash=1` added, and saves its stderr. Run it only with commands you would run anyway. `compare` only reads the two record files. Neither uses the network itself, but a go command you record follows your Go settings and may download modules.

A record holds file names, package import paths, directories, compiler and linker flags (including `-ldflags` values) and build settings of the run. Check it before you share it or upload it from a private project. Records don't contain file contents, only their hashes.

A wrong explanation is a normal bug. Report it privately only if it could lead someone into an unsafe action.
