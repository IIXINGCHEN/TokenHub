# Performance benchmark: tokenhub-local-stream

- Generated: 2026-08-10T01:28:07Z
- Commit: `25c481a387365846620375080b83795af6227add` (dirty working tree)
- Runtime: `go1.26.4 darwin/arm64`, 18 CPUs, Apple M5 Pro, 48.0 GiB RAM
- Scenario: `chat`, stream=true, concurrency=8, duration=2s, request=256 bytes

| Metric | Value |
| --- | ---: |
| Requests | 1067 |
| Success rate | 100.000% |
| Achieved throughput | 529.64 requests/s |
| Latency P50 / P95 / P99 | 14.903 / 16.132 / 17.136 ms |
| TTFT P50 / P95 / P99 | 5.773 / 6.702 / 7.769 ms |
| Estimated gateway TTFT P50 / P95 / P99 | 0.773 / 1.702 / 2.769 ms |
| Estimated gateway overhead P50 / P95 / P99 | 1.903 / 3.132 / 4.136 ms |

Estimated gateway overhead is end-to-end client latency minus configured fake-upstream latency, clamped at zero. It is an estimate, not an internal timer.
