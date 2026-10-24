# NHI Console - frontend image.
#
# Two stages. The first has Node and the whole dependency tree; the second has
# nginx and 24 static files. Nothing from the build stage is carried forward
# except `dist`, so node_modules, the lockfile and any build-time environment
# never reach a running container.
#
# Build:
#   docker build -t nhi-console:latest .
#
# Run - the backend upstream is chosen at RUN time:
#   docker run --rm -p 3000:3000 \
#     -e API_UPSTREAM=http://nhi-api:8080 \
#     -e DISCOVERY_UPSTREAM=http://discovery-scan:8080 \
#     nhi-console:latest
#
# The image holds no secrets: the browser signs in against the discovery API
# and sends its own bearer token, which nginx forwards unchanged.

# ── Stage 1: build the bundle ────────────────────────────────────────────────
FROM node:22-alpine AS build

WORKDIR /app

# The manifest and lockfile alone, so this layer - the slow one - is reused on
# any commit that does not change dependencies.
COPY package.json package-lock.json ./

# `npm ci` rather than `npm install`: it installs exactly the lockfile, fails
# if the two have drifted, and never rewrites the lockfile inside the image.
RUN npm ci

# Then the sources. Everything irrelevant is excluded by .dockerignore, which
# is also what keeps a local .env out of this layer.
COPY . .

# Build-time variable, with a safe default:
#   VITE_API_URL   left empty so the browser calls same-origin /api, which
#                  nginx proxies to the backend. Set it only if the backend is
#                  served from a different origin.
ARG VITE_API_URL=""
ENV VITE_API_URL=$VITE_API_URL
#   VITE_INTEGRATIONS_PREVIEW  "true" opens the whole console without a
#                  connected AWS account and runs the Integrations page on
#                  sample accounts in the browser (for review before the AWS
#                  side is deployed). Leave "false" in production.
ARG VITE_INTEGRATIONS_PREVIEW="false"
ENV VITE_INTEGRATIONS_PREVIEW=$VITE_INTEGRATIONS_PREVIEW

RUN npm run build

# A build that silently produced nothing would otherwise be discovered as a
# 404 in production, so fail here instead.
RUN test -f dist/index.html

# ── Stage 2: serve it ────────────────────────────────────────────────────────
FROM nginx:1.27-alpine AS runtime

# The image's own entrypoint substitutes variables in /etc/nginx/templates at
# start. Restricted to these names so that nginx's own $variables - $uri,
# $host, $proxy_host and the rest - are left alone.
ENV NGINX_ENVSUBST_OUTPUT_DIR=/etc/nginx/conf.d \
    NGINX_ENVSUBST_FILTER='(API_UPSTREAM|DNS_RESOLVER|DISCOVERY_UPSTREAM|DISCOVERY_API_KEY)$'

# Defaults for a compose stack with the backend service named nhi-api. Docker's
# embedded DNS lives at 127.0.0.11; override DNS_RESOLVER on Kubernetes.
# DISCOVERY_UPSTREAM is the separately deployed discovery scan service that the
# Sync button calls (through /scan-api/).
# DISCOVERY_API_KEY is deliberately NOT set here: a key must never be written
# into an image layer, not even an empty one. Pass it at run time (-e, a compose
# `environment:` entry, or a Kubernetes Secret). When it is absent,
# docker/18-default-env.envsh below sets it empty so the proxy sends no key.
ENV API_UPSTREAM="http://nhi-api:8080" \
    DNS_RESOLVER="127.0.0.11" \
    DISCOVERY_UPSTREAM="http://discovery-scan:8080"

# The default site would otherwise be served alongside ours on the same port.
RUN rm -f /etc/nginx/conf.d/default.conf

COPY nginx/nginx.conf.template /etc/nginx/templates/nhi-console.conf.template
COPY nginx/security-headers.conf /etc/nginx/snippets/security-headers.conf
# Sourced by the nginx entrypoint before the template is rendered.
COPY --chmod=0755 docker/18-default-env.envsh /docker-entrypoint.d/18-default-env.envsh

COPY --from=build /app/dist /usr/share/nginx/html

# Port 3000 - the API owns 8080, so the two can share a host or a pod without
# colliding - and the unprivileged user, so the container needs no root and no
# NET_BIND_SERVICE capability. `nginx` already exists in this image; the
# directories it writes to are chowned because a read-only root filesystem plus
# tmpfs mounts is a reasonable way to run this.
# /etc/nginx/conf.d is chowned too, and that is not cosmetic: the image's
# entrypoint writes the substituted template there at start, and as a non-root
# user it cannot write into a root-owned directory - the container would come
# up serving nothing.
RUN chown -R nginx:nginx \
        /usr/share/nginx/html \
        /var/cache/nginx \
        /var/log/nginx \
        /etc/nginx/conf.d \
    && touch /var/run/nginx.pid \
    && chown nginx:nginx /var/run/nginx.pid

USER nginx

EXPOSE 3000

# Served by nginx itself, so this reports on the web tier rather than on an
# upstream that may legitimately be down.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:3000/healthz || exit 1

STOPSIGNAL SIGQUIT

CMD ["nginx", "-g", "daemon off;"]
