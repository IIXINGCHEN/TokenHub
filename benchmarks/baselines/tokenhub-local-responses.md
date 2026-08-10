# Performance benchmark: tokenhub-local-responses

- Generated: 2026-08-10T01:28:05Z
- Commit: `25c481a387365846620375080b83795af6227add` (dirty working tree)
- Runtime: `go1.26.4 darwin/arm64`, 18 CPUs, Apple M5 Pro, 48.0 GiB RAM
- Scenario: `responses`, stream=false, concurrency=8, duration=2s, request=256 bytes

| Metric | Value |
| --- | ---: |
| Requests | 2116 |
| Success rate | 100.000% |
| Achieved throughput | 1054.22 requests/s |
| Latency P50 / P95 / P99 | 7.505 / 8.681 / 9.278 ms |
| Estimated gateway overhead P50 / P95 / P99 | 2.505 / 3.681 / 4.278 ms |

Estimated gateway overhead is end-to-end client latency minus configured fake-upstream latency, clamped at zero. It is an estimate, not an internal timer.
