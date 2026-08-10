# Performance benchmark: tokenhub-local-failover

- Generated: 2026-08-10T01:28:56Z
- Commit: `25c481a387365846620375080b83795af6227add` (dirty working tree)
- Runtime: `go1.26.4 darwin/arm64`, 18 CPUs, Apple M5 Pro, 48.0 GiB RAM
- Scenario: `chat`, stream=false, concurrency=8, duration=2s, request=256 bytes

| Metric | Value |
| --- | ---: |
| Requests | 1235 |
| Success rate | 100.000% |
| Achieved throughput | 614.00 requests/s |
| Latency P50 / P95 / P99 | 12.690 / 15.652 / 18.581 ms |
| Estimated gateway overhead P50 / P95 / P99 | 2.690 / 5.652 / 8.581 ms |

Estimated gateway overhead is end-to-end client latency minus configured fake-upstream latency, clamped at zero. It is an estimate, not an internal timer.
