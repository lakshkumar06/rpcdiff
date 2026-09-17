# rpcdiff

`rpcdiff` is a small **RPC migration workbench**. You point it at two Ethereum-compatible JSON-RPC HTTP endpoints, give it a list of JSON-RPC requests, and it tells you where the responses disagree.

It is inspired by research on API differencing (for example APIDiffer). It is **not** a reproduction of that paper. It does not generate requests, pin chain state, fuzz nodes, or decide which implementation is “correct.”

## Problem

When you migrate from one RPC provider or client to another (Geth → Erigon, a vendor endpoint → your own node, a new release of the same client), the same method can return a different hex encoding, a different error, a missing field, or a timeout. Grep-ing logs does not scale. `rpcdiff` runs a **fixed, user-supplied** request list against both URLs and classifies each pair of responses with deterministic rules.

## What the tool does

- POSTs each request in a JSON file to a **baseline** URL and a **candidate** URL
- Records HTTP status, JSON-RPC errors, latency, and raw bodies
- Recursively compares JSON (object key order ignored, array order matters, missing ≠ null)
- Applies **conservative, documented** hex normalization for six common methods
- Writes a terminal summary, a JSON report, and an optional HTML table

## What it does not prove

- That two Ethereum implementations are semantically equivalent
- That the chain head, mempool, or pending state match
- That a mismatch is a bug (some clients encode `0x0` vs `0x00` on purpose)
- Coverage of the JSON-RPC surface (only the requests you supply are tested)

## Installation

Requires Go 1.22+.

```bash
git clone <this-repo>
cd project
go test ./...
go build -o rpcdiff ./cmd/rpcdiff
```

## Usage

```bash
rpcdiff compare \
  --baseline http://localhost:8545 \
  --candidate http://localhost:9545 \
  --requests examples/requests.json \
  --output report.json \
  --html report.html \
  --timeout 5s \
  --workers 4
```

Local demo **without an Ethereum node** (starts two fake RPC servers):

```bash
go run ./cmd/rpcdiff demo --output report.json --html report.html
```

Or: `make demo`

## CI migration gate

Use `gate` before changing the RPC endpoint used by an application. It runs the
realistic `examples/public-requests.json` suite by default, writes both a
machine-readable JSON report and an HTML compatibility report, and exits with
status 1 if any request is not a `MATCH`.

```bash
go run ./cmd/rpcdiff gate \
  --baseline "$OLD_RPC_URL" \
  --candidate "$NEW_RPC_URL" \
  --output migration-report.json \
  --html migration-report.html
```

Provider-specific JSON-RPC error wording is ignored by the gate by default;
error codes, error data, results, response shape, timeouts, and malformed
responses still fail it. Use `--strict-errors` to compare error messages too.
HTTP 301, 429, and 503 responses plus request timeouts are retried three times
with exponential backoff. If the temporary failure persists, the report marks
the request `TRANSIENT_FAILURE` so it is distinguishable from an incompatibility.
Upload `migration-report.html` as a CI artifact for a readable per-request
report, or use the JSON file for annotations and automation.

## GitHub Actions CI

`.github/workflows/ci.yml` runs on every push and pull request. It runs the unit
tests, starts deterministic baseline and candidate fixture providers, and runs
the migration gate against `examples/ci-requests.json`. The workflow uploads
the JSON and HTML reports (plus fixture logs) as the `migration-reports`
artifact. Because the gate exits non-zero for any non-`MATCH` result, a broken
compatibility contract fails CI.

The public portfolio suite remains `examples/public-requests.json`; its 16
requests cover valid reads plus deliberate error and pending-state cases. The
CI-specific suite is intentionally small and deterministic so CI does not
depend on public RPC availability or moving blockchain state.

## Real endpoint suites

`examples/public-requests.json` is the 16-request portfolio suite. It covers
chain identity, blocks, transactions, receipts, logs, balances, contract
calls, deliberate error handling, and pending-state queries. Run it against
two public endpoints with:

```bash
./rpcdiff compare \
  --baseline https://public.1rpc.io/eth \
  --candidate https://ethereum-rpc.publicnode.com \
  --requests examples/public-requests.json \
  --output report-public.json \
  --html report-public.html \
  --timeout 15s \
  --workers 2
```

`examples/failure-requests.json` is kept separate for robustness checks. It contains a fake method and invalid parameters; use it as a second, explicitly failure-oriented run rather than mixing those results into the compatibility portfolio report.

## Request-file format

A JSON array of JSON-RPC 2.0 objects:

```json
[
  {
    "id": 1,
    "jsonrpc": "2.0",
    "method": "eth_blockNumber",
    "params": []
  }
]
```

`jsonrpc` defaults to `"2.0"` if omitted. `params` defaults to `[]`. `method` is required.

## Comparison semantics

1. **Transport first.** If either side times out → `TIMEOUT`. If HTTP status ≥ 400 or the body is not a JSON-RPC envelope → `INVALID_RESPONSE`. If the request itself is invalid → `INCONCLUSIVE`.
2. **Errors vs results.** JSON-RPC `error` objects are compared separately from `result`. One error and one result → `ERROR_MISMATCH`. Two errors that differ → `ERROR_MISMATCH`. Two identical errors → `MATCH`.
3. **JSON values.** Primitive equality is exact (string `"1"` ≠ number `1`; number literal `1` ≠ `1.0`). Objects are compared by key, not key order. Arrays are compared in order. A field that is JSON `null` is **not** the same as a field that is absent → `SHAPE_MISMATCH` (`missing_vs_null`).
4. **Envelope.** Classification uses `result` / `error` only. Echoed `id` / `jsonrpc` differences are not treated as method mismatches.
5. **Normalization (only these methods):**
   - `eth_blockNumber`, `eth_getBalance` — QUANTITY hex: lowercase, strip leading zeros (`0x00` → `0x0`)
   - `eth_getCode`, `eth_getStorageAt` — DATA hex: lowercase only; **no padding or trimming**
   - `eth_getBlockByNumber`, `eth_getTransactionReceipt` — known QUANTITY fields canonicalized; known hash/address/DATA fields lowercased; extra fields kept; log/transaction **array order is not sorted**

   Unknown methods are compared as-is. Normalization never drops a field to force a match.

### Classifications

| Class | Meaning |
| --- | --- |
| `MATCH` | Results (or errors) equal after the rules above |
| `VALUE_MISMATCH` | Same shape, different primitive/array item |
| `ERROR_MISMATCH` | Error presence or error object differs |
| `SHAPE_MISMATCH` | Type, array length, extra/missing field, or null vs missing |
| `TIMEOUT` | Either HTTP call exceeded `--timeout` |
| `TRANSIENT_FAILURE` | 301, 429, 503, or timeout persisted after retries |
| `INVALID_RESPONSE` | HTTP error or malformed / non-JSON-RPC body |
| `INCONCLUSIVE` | Request could not be compared (invalid input, cancelled run) |

Difference paths look like `result.balance`, `result.logs[0].address`, or `error.code`.

## Example output

```
rpcdiff compare  http://127.0.0.1:1234 vs http://127.0.0.1:5678
total requests           10
matches                  4
compatibility mismatches 4
transport failures       2  (timeout / invalid / inconclusive)
by category
  ERROR_MISMATCH     1
  INVALID_RESPONSE   1
  MATCH              4
  SHAPE_MISMATCH     1
  TIMEOUT            1
  VALUE_MISMATCH     2
```

The JSON report includes run metadata, both URLs, a UTC timestamp, and one result object per request.

The demo is wired so you can see every category: padded hex that matches after QUANTITY rules, a storage slot value mismatch, a null-vs-missing block field, swapped receipt logs, an error vs a result, a hung `eth_chainId`, and a broken `eth_syncing` body.

## Limitations

- No auth headers (and the client will not log secrets)
- No batch JSON-RPC arrays
- No WebSocket / IPC
- No chain pinning (`eth_call` at a block hash you did not supply)
- Conservative normalization only; hex DATA length differences are mismatches
- Latency numbers are wall-clock and not a benchmark
- Fake servers in `internal/fixtures` are for demos and tests, not protocol completeness

## Future extensions

- Pinned block state (replay at a single `blockHash`)
- Schema-guided request generation
- Minimized reproducers for the first mismatch
- LLM-assisted explanations of a classified diff (never silent hiding)

## Layout

```
cmd/rpcdiff/          CLI (compare, demo)
internal/rpc/         HTTP JSON-RPC client and request files
internal/compare/     Recursive JSON compare + classification
internal/normalize/   Method-aware hex rules
internal/report/      Terminal, JSON, HTML
internal/app/         Orchestration
internal/fixtures/    Fake RPC servers
examples/requests.json
```
