# TypeScript Worker Sample

Sample Temporal worker in TypeScript demonstrating large file processing with remote codec.

## Features

- Large file processing workflow
- Remote codec integration via HTTP
- Example activities for file processing
- Client for testing with 5MB payload

## Installation

```bash
npm install
```

## Building

```bash
npm run build
```

## Running

### Start Worker

```bash
export TEMPORAL_ADDRESS=localhost:7233
export CODEC_SERVER_URL=http://localhost:8080
npm run dev
```

### Run Client

```bash
npm run client
```

## Workflow

The `largeFileProcessor` workflow:
1. Receives large file data (5MB+)
2. Processes the file (simulated)
3. Uploads to destination
4. Returns result with processing time

## Activities

- `processLargeFile`: Simulates file processing
- `uploadToDestination`: Simulates upload to final destination

## Docker

```bash
docker build -t temporal-ts-worker .
docker run -e TEMPORAL_ADDRESS=temporal:7233 \
           -e CODEC_SERVER_URL=http://codec-server:8080 \
           temporal-ts-worker:latest
```

## Development

```bash
# Watch mode
npm run start.watch

# Lint
npm run lint

# Format
npm run format
```
