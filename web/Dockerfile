# ==============================================================================
# Multi-Stage Production Dockerfile for Backup Platform Frontend (Next.js)
# ==============================================================================

# Stage 1: Install dependencies
FROM node:22-alpine AS deps
RUN apk add --no-cache libc6-compat
WORKDIR /app

# Enable Corepack and prepare pnpm matching package.json (pnpm@11.25.0)
ENV COREPACK_DEFAULT_TO_LATEST=0
RUN corepack enable && corepack prepare pnpm@11.25.0 --activate

# Copy dependency manifests
COPY package.json pnpm-lock.yaml* .npmrc* pnpm-workspace.yaml* ./

# Install production and build dependencies with frozen lockfile
RUN pnpm install --frozen-lockfile

# Stage 2: Build Next.js application
FROM node:22-alpine AS builder
WORKDIR /app

ENV COREPACK_DEFAULT_TO_LATEST=0
RUN corepack enable && corepack prepare pnpm@11.25.0 --activate

COPY --from=deps /app/node_modules ./node_modules
COPY . .

ENV NEXT_TELEMETRY_DISABLED=1
ENV NODE_ENV=production

ARG BACKEND_ORIGIN=http://app:8080
ENV BACKEND_ORIGIN=${BACKEND_ORIGIN}

# Build standalone Next.js bundle
RUN pnpm build

# Stage 3: Hardened minimal production runner
FROM node:22-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

# Dedicated non-root service account (UID:GID 10001:10001)
RUN addgroup -g 10001 -S nodejs && \
    adduser -u 10001 -S nextjs -G nodejs

# Copy standalone output and static assets from builder
COPY --from=builder --chown=10001:10001 /app/.next/standalone ./
COPY --from=builder --chown=10001:10001 /app/.next/static ./.next/static

# Switch to non-root user
USER 10001:10001

# Document listening port
EXPOSE 3000

# Container health probe using native Node.js fetch
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD node -e "fetch('http://127.0.0.1:3000/login').then(r => { if (!r.ok) process.exit(1) }).catch(() => process.exit(1))"

CMD ["node", "server.js"]
