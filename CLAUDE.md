# Go Specification
- use go conventions
- use few arguments for the function, good practice if there is more then 3 arguments use config struct
- Immutability: don't mutate caller slices.
- Comments: package docs + **use-case docstrings** describing intent, not implementation.
- Testing: table-driven tests for each step; fakes for ports; contract tests for adapters;
- Concurrency: only where it reduces latency; guard with context; do not start goroutines you can't cancel.
- If http router is necessary, use standard library. Use fiber http if standard library is not enough.
