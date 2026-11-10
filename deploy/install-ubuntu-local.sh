#!/usr/bin/env bash
set -Eeuo pipefail

umask 077

if [[ ${EUID} -ne 0 ]]; then
  echo "This installer must run as root." >&2
  exit 1
fi

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PROJECT_DIR=${PROJECT_DIR:-$(cd "${SCRIPT_DIR}/.." && pwd)}
SOURCE_BINARY=${SOURCE_BINARY:-${PROJECT_DIR}/bin/wvc}
SERVICE_USER=wvc
SERVICE_DIR=/var/lib/webvirtcloud-go
CONFIG_DIR=/etc/webvirtcloud-go
ENV_FILE=${CONFIG_DIR}/env
DB_NAME=wvc
DB_USER=wvc
PANEL_ADDR=${WVC_PANEL_ADDR:-0.0.0.0:8080}
IMAGE_DIR=${WVC_IMAGE_DIR:-/var/lib/libvirt/images}
GRANT_IMAGE_ACCESS=${WVC_GRANT_IMAGE_ACCESS:-0}

if [[ ! -x ${SOURCE_BINARY} ]]; then
  echo "Built binary not found or not executable: ${SOURCE_BINARY}" >&2
  exit 1
fi

if [[ ! -f ${PROJECT_DIR}/deploy/wvc.service ]]; then
  echo "Service file not found under ${PROJECT_DIR}/deploy" >&2
  exit 1
fi

echo "[1/8] Installing PostgreSQL"
export DEBIAN_FRONTEND=noninteractive
apt-get update
packages=(ca-certificates curl openssl postgresql postgresql-client)
if [[ ${GRANT_IMAGE_ACCESS} == 1 ]]; then
  packages+=(acl)
fi
if [[ ${WVC_INSTALL_EXTENDED_TOOLS:-0} == 1 ]]; then
  packages+=(
    guestfs-tools libguestfs-tools virtinst virt-v2v virt-what virt-top
    libvirt-login-shell virt-p2v
  )
fi
apt-get install -y --no-install-recommends "${packages[@]}"
systemctl enable --now postgresql
systemctl enable --now libvirtd
systemctl is-active --quiet libvirtd

echo "[2/8] Creating service user and directories"
if ! id "${SERVICE_USER}" >/dev/null 2>&1; then
  useradd --system --user-group --groups libvirt \
    --create-home --home-dir "${SERVICE_DIR}" \
    --shell /usr/sbin/nologin "${SERVICE_USER}"
else
  usermod -aG libvirt "${SERVICE_USER}"
fi

install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 0750 "${SERVICE_DIR}"
install -d -o "${SERVICE_USER}" -g "${SERVICE_USER}" -m 0700 \
  "${SERVICE_DIR}/data" "${SERVICE_DIR}/.ssh" "${SERVICE_DIR}/tmp"
install -d -o root -g "${SERVICE_USER}" -m 0750 "${CONFIG_DIR}"
if [[ ${GRANT_IMAGE_ACCESS} == 1 ]]; then
  if [[ ! -d ${IMAGE_DIR} ]]; then
    echo "WVC_GRANT_IMAGE_ACCESS=1 requires an existing image directory: ${IMAGE_DIR}" >&2
    exit 1
  fi
  setfacl -R -m "u:${SERVICE_USER}:rwX" "${IMAGE_DIR}"
  setfacl -d -m "u:${SERVICE_USER}:rwx" "${IMAGE_DIR}"
  echo "Granted reviewed ACL access to ${SERVICE_USER} for ${IMAGE_DIR}"
fi

echo "[3/8] Preparing PostgreSQL database"
if [[ ! -s ${ENV_FILE} ]]; then
  DB_PASSWORD=$(openssl rand -hex 24)
  if [[ $(runuser -u postgres -- psql -tAc \
    "SELECT 1 FROM pg_roles WHERE rolname='${DB_USER}'") == "1" ]]; then
    runuser -u postgres -- psql -v ON_ERROR_STOP=1 -c \
      "ALTER ROLE ${DB_USER} WITH LOGIN PASSWORD '${DB_PASSWORD}';"
  else
    runuser -u postgres -- psql -v ON_ERROR_STOP=1 -c \
      "CREATE ROLE ${DB_USER} WITH LOGIN PASSWORD '${DB_PASSWORD}';"
  fi

  if [[ $(runuser -u postgres -- psql -tAc \
    "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'") != "1" ]]; then
    runuser -u postgres -- createdb --owner="${DB_USER}" "${DB_NAME}"
  fi

  {
    printf '%s\n' \
      "WVC_ADDR=${PANEL_ADDR}" \
      "WVC_DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@127.0.0.1:5432/${DB_NAME}?sslmode=disable" \
      "WVC_MASTER_KEY_FILE=${SERVICE_DIR}/data/master.key" \
      "WVC_LOCAL_IMAGE_DIR=${IMAGE_DIR}" \
      "WVC_ADMIN_USERNAME=admin" \
      "WVC_ADMIN_PASSWORD=" \
      "WVC_WORKERS=2" \
      "WVC_SECURE_COOKIES=false" \
      "WVC_TRUST_PROXY=false" \
      "WVC_LOG_LEVEL=info" \
      "WVC_LOG_FORMAT=text" \
      "WVC_ALLOW_INSECURE_VNC=false"
  } >"${ENV_FILE}"
  chown root:"${SERVICE_USER}" "${ENV_FILE}"
  chmod 0640 "${ENV_FILE}"
  unset DB_PASSWORD
else
  echo "Keeping existing ${ENV_FILE}"
fi

echo "[4/8] Installing application and systemd unit"
install -o root -g root -m 0755 "${SOURCE_BINARY}" /usr/local/bin/wvc
install -o root -g root -m 0644 \
  "${PROJECT_DIR}/deploy/wvc.service" \
  /etc/systemd/system/webvirtcloud-go.service
install -d -o root -g root -m 0755 /etc/polkit-1/rules.d
install -o root -g root -m 0644 \
  "${PROJECT_DIR}/deploy/60-webvirtcloud-go.rules" \
  /etc/polkit-1/rules.d/60-webvirtcloud-go.rules
systemctl daemon-reload
systemctl restart polkit

echo "[5/8] Starting WebVirtCloud Go"
systemctl enable webvirtcloud-go
systemctl restart webvirtcloud-go

echo "[6/8] Waiting for readiness"
ready=false
for _ in $(seq 1 90); do
  if curl -fsS --max-time 3 http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
    ready=true
    break
  fi
  if ! systemctl is-active --quiet webvirtcloud-go; then
    break
  fi
  sleep 1
done

if [[ ${ready} != true ]]; then
  systemctl status webvirtcloud-go --no-pager || true
  journalctl -u webvirtcloud-go -n 100 --no-pager || true
  echo "WebVirtCloud Go did not become ready." >&2
  exit 1
fi

echo "[7/8] Verifying libvirt access as the service user"
runuser -u "${SERVICE_USER}" -- virsh -c qemu:///system list --all

echo "[8/8] Network summary"
if systemctl is-active --quiet ufw 2>/dev/null; then
  IFS=',' read -r -a allow_cidrs <<<"${WVC_UFW_ALLOW_CIDRS:-}"
  for cidr in "${allow_cidrs[@]}"; do
    cidr=${cidr//[[:space:]]/}
    if [[ -n ${cidr} ]]; then
      ufw allow from "${cidr}" to any port 8080 proto tcp
    fi
  done
  ufw status
else
  echo "UFW is not active; no firewall changes made."
fi

systemctl --no-pager --full status webvirtcloud-go | sed -n '1,20p'
curl -fsS http://127.0.0.1:8080/readyz
echo
lan_ip=$(hostname -I | awk '{print $1}')
tailscale_ip=$(ip -4 -brief address show dev tailscale0 2>/dev/null |
  awk 'NR == 1 {split($3, parts, "/"); print parts[1]}')
echo "Panel URL (LAN): http://${lan_ip}:8080"
if [[ -n ${tailscale_ip} ]]; then
  echo "Panel URL (Tailscale): http://${tailscale_ip}:8080"
fi

initial_password_file=$(find "${SERVICE_DIR}/data" -maxdepth 1 \
  -name 'initial-admin-password-*.txt' -print -quit)
if [[ -n ${initial_password_file} ]]; then
  echo "Initial admin password file: ${initial_password_file}"
  echo "Read it locally with: sudo cat ${initial_password_file}"
  echo "The password is intentionally not printed here so installer logs do not capture it."
else
  echo "No one-time admin password file is present."
fi
