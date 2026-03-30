# Load Tests

Load tests using [k6](https://k6.io/).

## Prerequisites

```bash
brew install k6
```

## Running

### Job creation throughput

Ramps up to 100 VUs creating jobs over 3 minutes:

```bash
# Against local dev server
k6 run job_create.js

# Against production (requires auth token)
K6_BASE_URL=https://job.enkiduck.com K6_AUTH_TOKEN=<token> k6 run job_create.js
```

### Job claim throughput

Seeds pending jobs, then monitors how fast the scheduler claims and executes them:

```bash
k6 run job_claim.js
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `K6_BASE_URL` | `http://localhost:8080` | API base URL |
| `K6_AUTH_TOKEN` | (empty) | Bearer token for authenticated requests |
