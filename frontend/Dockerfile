FROM node:22-alpine AS build
WORKDIR /app

# Copy package files first for better layer caching
COPY package.json package-lock.json ./

# Install all dependencies (including devDependencies for build)
RUN mkdir -p public && npm ci && npm cache clean --force

# Copy source code after dependencies are installed
COPY . .

# Set default SITE_URL for sitemap generation
ENV SITE_URL=https://starlight-ai.freemyip.com

# Build the application
RUN npm run build

# Production stage
FROM nginx:stable-alpine

# Copy built assets from build stage
COPY --from=build /app/build /usr/share/nginx/html

# Copy nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Security best practices
RUN addgroup -g 1001 -S stargate && \
    adduser -S stargate -u 1001 && \
    chown -R stargate:stargate /usr/share/nginx/html && \
    chown -R stargate:stargate /var/cache/nginx && \
    chown -R stargate:stargate /var/log/nginx && \
    chown -R stargate:stargate /etc/nginx/conf.d && \
    touch /var/run/nginx.pid && \
    chown -R stargate:stargate /var/run/nginx.pid

USER stargate

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
