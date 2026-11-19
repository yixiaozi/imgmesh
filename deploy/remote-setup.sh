#!/bin/bash
# Idempotent install for the imgmesh API on the existing VPS.
# Expects /usr/local/bin/imgmesh-api and /var/www/imgmesh/s/ to already be in place.
set -euo pipefail

ENV_FILE=/etc/imgmesh/api.env
install -d -m 755 /etc/imgmesh /var/lib/imgmesh/blobs /var/www/imgmesh/s

if ! id imgmesh >/dev/null 2>&1; then
  useradd --system --home /var/lib/imgmesh --shell /usr/sbin/nologin imgmesh
fi
chown -R imgmesh:imgmesh /var/lib/imgmesh

if [ ! -f "$ENV_FILE" ]; then
  PW="$(openssl rand -hex 24)"
  SECRET="$(openssl rand -base64 48 | tr '+/' '-_' | tr -d '=')"
  umask 077
  cat > "$ENV_FILE" <<EOF
DATABASE_URL=postgres://imgmesh:${PW}@127.0.0.1:5437/imgmesh?sslmode=disable
BLOB_SECRET=${SECRET}
PUBLIC_BASE_URL=https://imgmesh.com
BLOB_DIR=/var/lib/imgmesh/blobs
APPLE_AUDIENCE=com.imgmesh.app
APPLE_TEAM_ID=
ADDR=127.0.0.1:8097
EOF
  unset PW SECRET
fi
if ! grep -q '^ADMIN_TOKEN=.' "$ENV_FILE"; then
  TOKEN="$(openssl rand -hex 24)"
  printf '\nADMIN_TOKEN=%s\n' "$TOKEN" >> "$ENV_FILE"
  unset TOKEN
fi
chmod 600 "$ENV_FILE"
chown root:root "$ENV_FILE"

PW="$(sed -n 's#^DATABASE_URL=postgres://imgmesh:\([^@]*\)@.*#\1#p' "$ENV_FILE")"
if [ -z "$PW" ]; then
  echo "api.env is missing a postgres password" >&2
  exit 1
fi

if ! docker ps -a --format '{{.Names}}' | grep -qx imgmesh-postgres; then
  docker run -d --name imgmesh-postgres \
    --restart unless-stopped \
    --memory 256m --memory-swap 256m \
    -e POSTGRES_PASSWORD="$PW" \
    -e POSTGRES_USER=imgmesh \
    -e POSTGRES_DB=imgmesh \
    -p 127.0.0.1:5437:5432 \
    -v imgmesh-pgdata:/var/lib/postgresql/data \
    postgres:16-alpine \
    postgres -c max_connections=20 -c shared_buffers=64MB
fi
unset PW
docker start imgmesh-postgres >/dev/null
for _ in $(seq 1 30); do
  if docker exec imgmesh-postgres pg_isready -U imgmesh -d imgmesh >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec imgmesh-postgres pg_isready -U imgmesh -d imgmesh >/dev/null

install -m 644 /tmp/imgmesh-api.service /etc/systemd/system/imgmesh-api.service
systemctl daemon-reload
systemctl enable imgmesh-api
systemctl restart imgmesh-api

python3 - <<'PY'
from pathlib import Path
path = Path("/etc/nginx/sites-available/imgmesh")
text = path.read_text()
block = Path("/tmp/nginx-imgmesh-locations.conf").read_text().strip("\n")
begin = "    # imgmesh-app begin"
end = "    # imgmesh-app end"
if begin in text and end in text:
    pre, rest = text.split(begin, 1)
    _, post = rest.split(end, 1)
    text = pre + block + post
else:
    lines = text.splitlines(keepends=True)
    depth = 0
    start = None
    insert_at = None
    for i, line in enumerate(lines):
        if "server {" in line and depth == 0:
            start = i
        depth += line.count("{") - line.count("}")
        if start is not None and depth == 0:
            chunk = "".join(lines[start:i+1])
            if "listen 443 ssl" in chunk:
                insert_at = i
            start = None
    if insert_at is None:
        raise SystemExit("could not find the 443 server block")
    lines.insert(insert_at, block + "\n")
    text = "".join(lines)
path.write_text(text)
PY

nginx -t
systemctl reload nginx
for _ in $(seq 1 20); do
  if curl -fsS http://127.0.0.1:8097/v1/health; then
    echo
    exit 0
  fi
  sleep 0.5
done
journalctl -u imgmesh-api -n 40 --no-pager >&2 || true
exit 1
