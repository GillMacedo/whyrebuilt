# 0003: The go command's own hash inputs are the only evidence

**Decision:** whyrebuilt explains a cache miss only from the inputs the go command printed for each key. It doesn't inspect the environment, CI files or the cache directory beyond counting entries.

**Why:** the cache key is exactly the hash of those inputs, so a difference in them is a sure cause and an equal key is a sure match. Guessing from environment variables or CI settings would report causes that don't affect the key. Each cause in the report names the input line that changed, so the user can check it.

**Consequence:** if every key matches and B still rebuilt, the records can't say why beyond the cache entry count. The report says so instead of guessing.
