# Stage 1: Install all dependencies (development + production)
FROM node:20-alpine AS deps
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

# Stage 2: Install ONLY production dependencies (fully cached and isolated)
FROM node:20-alpine AS prod-deps
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --omit=dev

# Stage 3: Build the SvelteKit application
FROM node:20-alpine AS builder
WORKDIR /app

# Install curl and buf
RUN apk add --no-cache curl && \
    BIN="/usr/local/bin" && \
    VERSION="1.32.1" && \
    curl -sSL "https://github.com/bufbuild/buf/releases/download/v${VERSION}/buf-$(uname -s)-$(uname -m)" -o "${BIN}/buf" && \
    chmod +x "${BIN}/buf"

COPY . .
WORKDIR /app/frontend
COPY --from=deps /app/frontend/node_modules ./node_modules

# Generate Proto
RUN npm run proto:generate

# Build the application with the Node.js adapter, using BuildKit cache mounts for the Vite/Svelte compiler
RUN --mount=type=cache,target=/app/frontend/.svelte-kit \
    npm run build

# Stage 4: Production runtime
FROM node:20-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production

# Create non-root user
RUN addgroup --system --gid 1001 sveltekit && adduser --system --uid 1001 sveltekit

# Copy the built application
COPY --from=builder --chown=sveltekit:sveltekit /app/frontend/build ./build

# Copy production-only dependencies from prod-deps stage (fully cached!)
COPY --from=prod-deps --chown=sveltekit:sveltekit /app/frontend/node_modules ./node_modules

# Copy package.json for module resolution
COPY --chown=sveltekit:sveltekit frontend/package.json .

USER sveltekit

EXPOSE 3000
ENV PORT=3000
ENV HOST=0.0.0.0

# Start the SvelteKit server
CMD ["node", "build/index.js"]
