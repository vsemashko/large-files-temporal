# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./
COPY tsconfig.json ./

# Install dependencies
RUN npm ci

# Copy source code
COPY src ./src

# Build TypeScript code
RUN npm run build

# Runtime stage
FROM node:20-alpine

WORKDIR /app

# Copy package files and install production dependencies only
COPY package*.json ./
RUN npm ci --production

# Copy built code from builder
COPY --from=builder /app/lib ./lib

# Run the worker
CMD ["node", "lib/worker.js"]
