# Autoresearch Dashboard: parallel runner efficiency

**Runs:** 8 | **Kept:** 6 | **Discarded:** 2 | **Crashed:** 0
**Baseline:** `parallel_flow_ns_per_op`: 796991230ns/op (#1)
**Best:** `parallel_flow_ns_per_op`: 56074748ns/op (#8, -93.0%)

| # | commit | parallel_flow_ns_per_op | status | description |
|---|--------|-------------------------|--------|-------------|
| 1 | 3705141 | 796991230ns/op (+0.0%) | keep | baseline |
| 2 | fc435aa | 167201083ns/op (-79.0%) | keep | execute independent ready nodes concurrently |
| 3 | 14c48f9 | 129007182ns/op (-83.8%) | keep | serialize observer callbacks for parallel execution |
| 4 | 14c48f9 | 222101774ns/op (-72.1%) | discard | precompile template regexes only |
| 5 | 14c48f9 | 131964369ns/op (-83.4%) | discard | reuse a shared http client in request nodes |
| 6 | 948b0aa | 57957945ns/op (-92.7%) | keep | fast-path singleton ready batches |
| 7 | 2fd3963 | 57640743ns/op (-92.8%) | keep | preserve flow order in ready node scans |
| 8 | 2170ad5 | 56074748ns/op (-93.0%) | keep | make AllOutputs a read-only snapshot view |
